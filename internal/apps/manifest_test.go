package apps

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"

	"gitbucket/internal/db"
)

func TestCreateAndConsumeManifestConversion(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	code, err := CreateManifestConversion(ctx, fs, "app-id-xyz")
	if err != nil {
		t.Fatalf("CreateManifestConversion: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionManifestConversions).Doc(code).Delete(context.Background())
	})
	if code == "" {
		t.Fatal("code should be non-empty")
	}

	appID, err := ConsumeManifestConversion(ctx, fs, code)
	if err != nil {
		t.Fatalf("ConsumeManifestConversion: %v", err)
	}
	if appID != "app-id-xyz" {
		t.Errorf("appID = %q, want app-id-xyz", appID)
	}

	// Second consume must fail — single-use.
	if _, err := ConsumeManifestConversion(ctx, fs, code); err == nil {
		t.Error("expected error on second Consume (single-use)")
	}
}

func TestConsumeManifestConversion_NotFound(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	_, err := ConsumeManifestConversion(ctx, fs, "no-such-code")
	if err == nil {
		t.Error("expected error for unknown code")
	}
}

func TestConsumeManifestConversion_Expired(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	code, _ := CreateManifestConversion(ctx, fs, "app-id-exp")
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionManifestConversions).Doc(code).Delete(context.Background())
	})

	// Backdate expires_at directly via firestore.Update.
	_, err := fs.Collection(CollectionManifestConversions).Doc(code).Update(ctx,
		[]firestore.Update{{Path: "expires_at", Value: time.Now().Add(-time.Hour).UTC()}})
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}

	if _, err := ConsumeManifestConversion(ctx, fs, code); err == nil {
		t.Error("expected error for expired code")
	}
}
