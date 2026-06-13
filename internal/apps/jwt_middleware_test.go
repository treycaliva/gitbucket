// internal/apps/jwt_middleware_test.go
package apps

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestRequireAppJWTMiddleware(t *testing.T) {
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

	slug := "jwtm-app-" + randHex(4)
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
	handler := RequireAppJWT(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appFromCtx := AppFromContext(r.Context())
		if appFromCtx == nil {
			t.Error("expected App in context")
			http.Error(w, "no app", 500)
			return
		}
		w.WriteHeader(200)
	}))

	t.Run("missing auth", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		handler.ServeHTTP(rr, req)
		if rr.Code != 401 {
			t.Errorf("code = %d, want 401", rr.Code)
		}
	})

	t.Run("valid bearer", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now, now.Add(5*time.Minute))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		handler.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("code = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
		}
	})
}
