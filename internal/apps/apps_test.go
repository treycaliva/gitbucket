// internal/apps/apps_test.go
package apps

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"gitbucket/internal/db"
)

func TestCreateAndGetApp(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	store := NewMemorySecretStore()
	suffix := randHex(4)
	slug := "test-app-" + suffix
	owner := AccountRef{ID: "owner-uid-" + randHex(2), Type: AccountTypeUser}
	botUID, err := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}

	// register cleanup BEFORE anything that can fail
	t.Cleanup(func() {
		cctx := context.Background()
		docs, _ := fs.Collection(CollectionApps).Where("slug", "==", slug).Documents(cctx).GetAll()
		for _, d := range docs {
			_, _ = d.Ref.Delete(cctx)
		}
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		// Bot UID is created with random ID — best-effort delete via username doc lookup
		docsU, _ := fs.Collection(CollectionUsers).Where("username", "==", slug+"[bot]").Documents(cctx).GetAll()
		for _, d := range docsU {
			_, _ = d.Ref.Delete(cctx)
		}
		// Also delete by known botUID if we have it
		if botUID != "" {
			_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
		}
	})

	req := CreateAppRequest{
		Slug:               slug,
		Name:               "Test App",
		OwnerAccount:       owner,
		BotUserID:          botUID,
		WebhookURL:         "https://example.com/hook",
		DefaultPermissions: Permissions{"issues": PermWrite, "metadata": PermRead},
		DefaultEvents:      []string{"issues", "issue_comment"},
	}
	created, secrets, err := CreateApp(ctx, fs, store, req)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.AppID == "" {
		t.Fatal("AppID should be set")
	}
	if secrets.ClientSecret == "" || secrets.WebhookSecret == "" || secrets.PrivateKeyPEM == "" {
		t.Fatal("secrets should be populated exactly once at creation")
	}

	// PEM should parse as a valid RSA private key.
	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	if block == nil {
		t.Fatal("PrivateKeyPEM did not decode")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse pkcs1: %v", err)
	}
	if _, ok := any(key).(*rsa.PrivateKey); !ok {
		t.Fatal("parsed key is not *rsa.PrivateKey")
	}

	// Read back via GetApp.
	got, err := GetApp(ctx, fs, created.AppID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.Slug != slug || got.Name != "Test App" {
		t.Errorf("GetApp mismatch: %+v", got)
	}
	if got.PublicKeyPEM == "" {
		t.Error("PublicKeyPEM should be persisted")
	}

	// GetAppBySlug works too.
	bySlug, err := GetAppBySlug(ctx, fs, slug)
	if err != nil {
		t.Fatalf("GetAppBySlug: %v", err)
	}
	if bySlug.AppID != created.AppID {
		t.Errorf("GetAppBySlug returned %q, want %q", bySlug.AppID, created.AppID)
	}
}

func TestCreateApp_DuplicateSlug(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()
	slug := "dup-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}

	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")

	// register cleanup BEFORE anything that can fail
	t.Cleanup(func() {
		cctx := context.Background()
		docs, _ := fs.Collection(CollectionApps).Where("slug", "==", slug).Documents(cctx).GetAll()
		for _, d := range docs {
			_, _ = d.Ref.Delete(cctx)
		}
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		docsU, _ := fs.Collection(CollectionUsers).Where("username", "==", slug+"[bot]").Documents(cctx).GetAll()
		for _, d := range docsU {
			_, _ = d.Ref.Delete(cctx)
		}
		if botUID != "" {
			_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
		}
	})

	req := CreateAppRequest{Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL: "https://example.com/x"}
	if _, _, err := CreateApp(ctx, fs, store, req); err != nil {
		t.Fatalf("first CreateApp: %v", err)
	}
	if _, _, err := CreateApp(ctx, fs, store, req); err == nil {
		t.Error("expected duplicate slug to fail")
	}
}
