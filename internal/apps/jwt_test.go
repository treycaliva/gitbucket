package apps

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/golang-jwt/jwt/v4"
	"gitbucket/internal/db"
)

func signTestJWT(t *testing.T, appID string, key *rsa.PrivateKey, iat, exp time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": appID,
		"iat": iat.Unix(),
		"exp": exp.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

func TestVerifyAppJWT(t *testing.T) {
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

	slug := "jwt-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	var botUID string
	var app *App
	t.Cleanup(func() {
		cctx := context.Background()
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
	var secrets *AppSecrets
	app, secrets, err = CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse priv: %v", err)
	}

	verifier := NewJWTVerifier(fs, 60*time.Second)

	t.Run("valid", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now, now.Add(5*time.Minute))
		gotApp, err := verifier.Verify(ctx, tok)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if gotApp.AppID != app.AppID {
			t.Errorf("returned app_id = %s, want %s", gotApp.AppID, app.AppID)
		}
	})

	t.Run("expired", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now.Add(-10*time.Minute), now.Add(-5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for expired token")
		}
	})

	t.Run("exp window too wide", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now, now.Add(20*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for exp >10m after iat")
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		other, _ := rsa.GenerateKey(rand.Reader, 2048)
		now := time.Now()
		tok := signTestJWT(t, app.AppID, other, now, now.Add(5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected signature error for wrong key")
		}
	})

	t.Run("unknown issuer", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, "no-such-app", priv, now, now.Add(5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for unknown issuer")
		}
	})

	t.Run("suspended app", func(t *testing.T) {
		// Suspend the app.
		_, err := fs.Collection(CollectionApps).Doc(app.AppID).Update(ctx, []firestore.Update{
			{Path: "suspended_at", Value: time.Now().UTC()},
		})
		if err != nil {
			t.Fatalf("suspend app: %v", err)
		}
		verifier.InvalidateCache(app.AppID)

		tok := signTestJWT(t, app.AppID, priv, time.Now(), time.Now().Add(5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for suspended app")
		}

		// Un-suspend.
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Update(ctx, []firestore.Update{
			{Path: "suspended_at", Value: nil},
		})
		verifier.InvalidateCache(app.AppID)
	})

	t.Run("error messages are generic", func(t *testing.T) {
		// Try every failure path; all errors should be the same string.
		now := time.Now()
		cases := []string{
			signTestJWT(t, "no-such-app", priv, now, now.Add(5*time.Minute)),
			signTestJWT(t, app.AppID, priv, now.Add(-time.Hour), now.Add(-30*time.Minute)),
			signTestJWT(t, app.AppID, priv, now, now.Add(20*time.Minute)),
			"not.a.jwt",
		}
		other, _ := rsa.GenerateKey(rand.Reader, 2048)
		cases = append(cases, signTestJWT(t, app.AppID, other, now, now.Add(5*time.Minute)))

		var seen string
		for i, tok := range cases {
			_, err := verifier.Verify(ctx, tok)
			if err == nil {
				t.Errorf("case %d expected error", i)
				continue
			}
			if seen == "" {
				seen = err.Error()
				continue
			}
			if err.Error() != seen {
				t.Errorf("case %d error %q differs from earlier %q — error messages should be generic", i, err.Error(), seen)
			}
		}
	})
}
