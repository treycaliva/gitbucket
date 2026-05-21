package apps

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"gitbucket/internal/db"
)

func TestMintAndVerifyInstallationToken(t *testing.T) {
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

	slug := "tok-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	var botUID string
	var app *App
	var inst *Installation
	t.Cleanup(func() {
		cctx := context.Background()
		if inst != nil {
			_, _ = fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(cctx)
		}
		if app != nil {
			_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(cctx)
		}
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		if botUID != "" {
			_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
		}
	})

	botUID, err = CreateBotUser(ctx, fs, slug, slug, "", "pending")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}
	app, _, err = CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite, "contents": PermWrite},
		DefaultEvents:      []string{"issues"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, err = CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         Permissions{"issues": PermWrite, "contents": PermWrite},
		Events:              []string{"issues"},
	})
	if err != nil {
		t.Fatalf("CreateInstallation: %v", err)
	}

	t.Run("default mint", func(t *testing.T) {
		out, err := MintInstallationToken(ctx, fs, inst, MintRequest{})
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if !strings.HasPrefix(out.Plaintext, "ghs_") {
			t.Errorf("token prefix = %q, want ghs_", out.Plaintext[:4])
		}
		want := time.Now().Add(time.Hour)
		diff := out.Record.ExpiresAt.Sub(want)
		if diff < -5*time.Second || diff > 5*time.Second {
			t.Errorf("expires_at off by %s", diff)
		}
		v, err := VerifyInstallationToken(ctx, fs, out.Plaintext)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if v.InstallationID != inst.InstallationID {
			t.Errorf("verify returned installation %s, want %s", v.InstallationID, inst.InstallationID)
		}
	})

	t.Run("subset perms ok", func(t *testing.T) {
		out, err := MintInstallationToken(ctx, fs, inst, MintRequest{
			Permissions: Permissions{"issues": PermRead},
		})
		if err != nil {
			t.Fatalf("mint subset: %v", err)
		}
		if out.Record.Permissions["issues"] != PermRead {
			t.Errorf("perms = %v", out.Record.Permissions)
		}
	})

	t.Run("broader perms rejected", func(t *testing.T) {
		_, err := MintInstallationToken(ctx, fs, inst, MintRequest{
			Permissions: Permissions{"workflows": PermWrite},
		})
		if err == nil {
			t.Error("expected error for permission not granted to installation")
		}
	})

	t.Run("verify rejects expired token", func(t *testing.T) {
		out, err := MintInstallationToken(ctx, fs, inst, MintRequest{})
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		hash := sha256Hex(out.Plaintext)
		_, err = fs.Collection(CollectionInstallationTokens).Doc(hash).Update(ctx, []firestore.Update{
			{Path: "expires_at", Value: time.Now().Add(-time.Minute)},
		})
		if err != nil {
			t.Fatalf("backdate: %v", err)
		}
		if _, err := VerifyInstallationToken(ctx, fs, out.Plaintext); err == nil {
			t.Error("expected expired token to fail verification")
		}
	})

	t.Run("verify rejects unknown token", func(t *testing.T) {
		_, err := VerifyInstallationToken(ctx, fs, "ghs_definitely-not-real-token-string")
		if err == nil {
			t.Error("expected error for unknown token")
		}
	})

	t.Run("verify rejects malformed token", func(t *testing.T) {
		if _, err := VerifyInstallationToken(ctx, fs, "not-a-ghs-token"); err == nil {
			t.Error("expected error for malformed token (missing ghs_ prefix)")
		}
	})
}
