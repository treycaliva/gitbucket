package apps

import (
	"context"
	"os"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestCreateAndUpdateDelivery(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	rec, err := CreateDelivery(ctx, fs, CreateDeliveryInput{
		AppID:          "app-" + randHex(2),
		InstallationID: "inst-" + randHex(2),
		Event:          "pull_request",
		TargetURL:      "https://example.com/webhook",
		PayloadSHA256:  "abc123",
	})
	if err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionWebhookDeliveries).Doc(rec.DeliveryID).Delete(context.Background())
	})
	if rec.DeliveryID == "" {
		t.Fatal("DeliveryID empty")
	}
	if rec.Status != "pending" {
		t.Errorf("Status = %q, want pending", rec.Status)
	}
	if rec.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", rec.Attempts)
	}

	if err := UpdateDeliveryStatus(ctx, fs, rec.DeliveryID, DeliveryUpdate{
		Status:           "delivered",
		LastResponseCode: 200,
		Attempts:         1,
		LastAttemptedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpdateDeliveryStatus: %v", err)
	}

	got, err := GetDelivery(ctx, fs, rec.DeliveryID)
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if got.Status != "delivered" {
		t.Errorf("Status after update = %q", got.Status)
	}
	if got.LastResponseCode != 200 {
		t.Errorf("LastResponseCode = %d", got.LastResponseCode)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d", got.Attempts)
	}
}

func TestGetDeliveryNotFound(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	got, err := GetDelivery(ctx, fs, "no-such-delivery")
	if err != nil {
		t.Fatalf("GetDelivery error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing delivery")
	}
}
