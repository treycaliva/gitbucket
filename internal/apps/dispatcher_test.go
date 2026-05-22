package apps

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/db"
)

func TestDispatcher_RelaysAndMarksDelivered(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	// Fake App webhook receiver — captures the request.
	var captured *http.Request
	var capturedBody []byte
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer app.Close()

	deliv, err := CreateDelivery(ctx, fs, CreateDeliveryInput{
		AppID:          "app-x",
		InstallationID: "inst-x",
		Event:          "pull_request",
		TargetURL:      app.URL,
		PayloadSHA256:  "deadbeef",
	})
	if err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionWebhookDeliveries).Doc(deliv.DeliveryID).Delete(context.Background())
	})

	dh := NewDispatcherHandler(fs, "" /* no OIDC audience in tests */)
	r := chi.NewRouter()
	r.Post("/_internal/dispatch-webhook/{id}", dh.Dispatch)

	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest("POST", "/_internal/dispatch-webhook/"+deliv.DeliveryID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=fake")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("dispatcher returned %d, want 200", rr.Code)
	}
	if captured == nil {
		t.Fatal("fake App did not receive a request")
	}
	if string(capturedBody) != string(body) {
		t.Errorf("body relay mismatch: %q vs %q", capturedBody, body)
	}
	if captured.Header.Get("X-GitHub-Event") != "pull_request" {
		t.Error("X-GitHub-Event header not relayed")
	}
	if captured.Header.Get("X-Hub-Signature-256") != "sha256=fake" {
		t.Error("X-Hub-Signature-256 header not relayed")
	}

	// Delivery record should be updated.
	got, _ := GetDelivery(ctx, fs, deliv.DeliveryID)
	if got.Status != "delivered" {
		t.Errorf("Status = %q, want delivered", got.Status)
	}
	if got.LastResponseCode != 200 {
		t.Errorf("LastResponseCode = %d", got.LastResponseCode)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", got.Attempts)
	}
}

func TestDispatcher_5xxReturnsNon2xxForRetry(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer app.Close()

	deliv, _ := CreateDelivery(ctx, fs, CreateDeliveryInput{
		AppID: "app-y", InstallationID: "inst-y", Event: "push",
		TargetURL: app.URL, PayloadSHA256: "x",
	})
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionWebhookDeliveries).Doc(deliv.DeliveryID).Delete(context.Background())
	})

	dh := NewDispatcherHandler(fs, "")
	r := chi.NewRouter()
	r.Post("/_internal/dispatch-webhook/{id}", dh.Dispatch)

	req := httptest.NewRequest("POST", "/_internal/dispatch-webhook/"+deliv.DeliveryID, strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// We expect the dispatcher to surface the upstream non-2xx so Cloud Tasks retries.
	if rr.Code < 400 {
		t.Errorf("dispatcher returned %d, expected non-2xx to trigger retry", rr.Code)
	}

	got, _ := GetDelivery(ctx, fs, deliv.DeliveryID)
	if got.Status != "failed" {
		t.Errorf("Status = %q, want failed", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d", got.Attempts)
	}
}
