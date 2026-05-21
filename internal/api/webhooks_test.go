package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/db"
	"gitbucket/internal/sync"
)

func TestVerifyGitHubSignature(t *testing.T) {
	secret := []byte("webhooksecret")
	payload := []byte(`{"ref":"refs/heads/main"}`)

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expectedSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewBuffer(payload))
	req.Header.Set("X-Hub-Signature-256", expectedSignature)

	if !VerifyGitHubSignature(req, secret) {
		t.Error("VerifyGitHubSignature failed to verify a valid signature")
	}

	// Mismatched signature
	reqInvalid := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewBuffer(payload))
	reqInvalid.Header.Set("X-Hub-Signature-256", "sha256=invalidhash")
	if VerifyGitHubSignature(reqInvalid, secret) {
		t.Error("VerifyGitHubSignature verified an invalid signature")
	}

	// Missing header
	reqMissing := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewBuffer(payload))
	if VerifyGitHubSignature(reqMissing, secret) {
		t.Error("VerifyGitHubSignature verified a missing signature header")
	}
}

func TestHandleGitHubWebhook(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping Webhook handler tests: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer client.Close()

	apiHandler := NewAPIHandler(client, nil, "", "", nil, sync.NewKMSClient(nil, ""))

	r := chi.NewRouter()
	apiHandler.RegisterRoutes(r)

	secret := "webhooksecret"
	os.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	defer os.Unsetenv("GITHUB_WEBHOOK_SECRET")

	// Helper to send request and return response code
	sendWebhook := func(payload map[string]interface{}) int {
		body, _ := json.Marshal(payload)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewBuffer(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr.Code
	}

	// 1. Valid Webhook Payload
	payloadValid := map[string]interface{}{
		"ref":   "refs/heads/main",
		"after": "1111111111111111111111111111111111111111",
		"pusher": map[string]interface{}{
			"name": "dev-user",
		},
		"sender": map[string]interface{}{
			"login": "dev-user",
		},
		"repository": map[string]interface{}{
			"name": "test-repo",
			"owner": map[string]interface{}{
				"login": "test-owner",
			},
		},
	}
	code := sendWebhook(payloadValid)
	if code != http.StatusAccepted {
		t.Errorf("expected status 202 Accepted, got %d", code)
	}

	// 2. Discarded Webhook: Pusher is a sync bot
	payloadBotPusher := map[string]interface{}{
		"ref":   "refs/heads/main",
		"after": "2222222222222222222222222222222222222222",
		"pusher": map[string]interface{}{
			"name": "gitbucket-sync-bot",
		},
		"sender": map[string]interface{}{
			"login": "dev-user",
		},
		"repository": map[string]interface{}{
			"name": "test-repo",
			"owner": map[string]interface{}{
				"login": "test-owner",
			},
		},
	}
	code = sendWebhook(payloadBotPusher)
	if code != http.StatusNoContent {
		t.Errorf("expected status 204 No Content for bot pusher, got %d", code)
	}

	// 3. Discarded Webhook: Commits contain git trailer
	payloadTrailer := map[string]interface{}{
		"ref":   "refs/heads/main",
		"after": "3333333333333333333333333333333333333333",
		"pusher": map[string]interface{}{
			"name": "dev-user",
		},
		"sender": map[string]interface{}{
			"login": "dev-user",
		},
		"repository": map[string]interface{}{
			"name": "test-repo",
			"owner": map[string]interface{}{
				"login": "test-owner",
			},
		},
		"commits": []map[string]interface{}{
			{
				"message": "fix: update README\n\nSynced-By: GitBucket",
				"author": map[string]interface{}{
					"username": "dev-user",
				},
				"committer": map[string]interface{}{
					"username": "dev-user",
				},
			},
		},
	}
	code = sendWebhook(payloadTrailer)
	if code != http.StatusNoContent {
		t.Errorf("expected status 204 No Content for commit trailer, got %d", code)
	}

	// 4. Discarded Webhook: Recently pushed SHA
	sha := "4444444444444444444444444444444444444444"
	// Write SHA to Firestore recent_sync_pushes
	_, err = client.Collection("recent_sync_pushes").Doc(sha).Set(ctx, map[string]interface{}{
		"createdAt": time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to write SHA cache doc: %v", err)
	}

	payloadSHA := map[string]interface{}{
		"ref":   "refs/heads/main",
		"after": sha,
		"pusher": map[string]interface{}{
			"name": "dev-user",
		},
		"sender": map[string]interface{}{
			"login": "dev-user",
		},
		"repository": map[string]interface{}{
			"name": "test-repo",
			"owner": map[string]interface{}{
				"login": "test-owner",
			},
		},
	}
	code = sendWebhook(payloadSHA)
	if code != http.StatusNoContent {
		t.Errorf("expected status 204 No Content for recently pushed SHA, got %d", code)
	}
}
