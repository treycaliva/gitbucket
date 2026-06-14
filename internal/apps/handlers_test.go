// internal/apps/handlers_test.go
package apps

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gitbucket/internal/db"
	"github.com/go-chi/chi/v5"
)

func TestAppHandlersEndToEnd(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "h-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, secrets, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite, "contents": PermRead},
		DefaultEvents:      []string{"issues"},
	})
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
	})

	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         Permissions{"issues": PermWrite, "contents": PermRead},
		Events:              []string{"issues"},
	})
	t.Cleanup(func() {
		fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(ctx)
	})

	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	priv, _ := x509.ParsePKCS1PrivateKey(block.Bytes)

	h := NewHandler(fs, store, NewJWTVerifier(fs, 60*time.Second), nil)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	bearer := func() string {
		return "Bearer " + signTestJWT(t, app.AppID, priv, time.Now(), time.Now().Add(5*time.Minute))
	}

	t.Run("GET /api/v3/app returns metadata", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["slug"] != slug {
			t.Errorf("slug = %v", body["slug"])
		}
	})

	t.Run("GET /api/v3/app/installations lists", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app/installations", nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var list []map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &list)
		if len(list) != 1 {
			t.Errorf("len = %d, want 1", len(list))
		}
	})

	t.Run("GET /api/v3/app/installations/{id}", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app/installations/"+inst.InstallationID, nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("POST .../access_tokens mints a token", func(t *testing.T) {
		req := httptest.NewRequest("POST",
			"/api/v3/app/installations/"+inst.InstallationID+"/access_tokens",
			bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", bearer())
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 201 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		tok, _ := body["token"].(string)
		if !strings.HasPrefix(tok, "ghs_") {
			t.Errorf("token prefix wrong: %q", tok)
		}
		if body["expires_at"] == nil {
			t.Error("expires_at missing")
		}
		// Verify it actually works as an installation token.
		if _, err := VerifyInstallationToken(ctx, fs, tok); err != nil {
			t.Errorf("minted token does not verify: %v", err)
		}
	})

	t.Run("POST .../access_tokens with subset perms ok", func(t *testing.T) {
		body := bytes.NewBufferString(`{"permissions":{"issues":"read"}}`)
		req := httptest.NewRequest("POST",
			"/api/v3/app/installations/"+inst.InstallationID+"/access_tokens", body)
		req.Header.Set("Authorization", bearer())
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 201 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("POST .../access_tokens with super perms → 422", func(t *testing.T) {
		body := bytes.NewBufferString(`{"permissions":{"workflows":"write"}}`)
		req := httptest.NewRequest("POST",
			"/api/v3/app/installations/"+inst.InstallationID+"/access_tokens", body)
		req.Header.Set("Authorization", bearer())
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 422 {
			t.Errorf("code = %d body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("missing JWT → 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 401 {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("installation owned by other app → 404", func(t *testing.T) {
		// Create another App + Installation; the original JWT should not be
		// able to mint tokens for the second installation.
		slug2 := "h-app2-" + randHex(4)
		botUID2, _ := CreateBotUser(ctx, fs, slug2, slug2, "", "pending")
		app2, _, _ := CreateApp(ctx, fs, store, CreateAppRequest{
			Slug: slug2, Name: slug2, OwnerAccount: owner, BotUserID: botUID2,
			WebhookURL: "https://example.com/y",
		})
		t.Cleanup(func() {
			_, _ = fs.Collection(CollectionApps).Doc(app2.AppID).Delete(ctx)
			_, _ = fs.Collection("usernames").Doc(slug2 + "[bot]").Delete(ctx)
			_, _ = fs.Collection(CollectionUsers).Doc(botUID2).Delete(ctx)
		})
		inst2, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
			AppID: app2.AppID, Account: installeeAcct,
			RepositorySelection: "all",
			Permissions:         Permissions{"issues": PermRead},
		})
		t.Cleanup(func() {
			fs.Collection(CollectionInstallations).Doc(inst2.InstallationID).Delete(ctx)
		})

		req := httptest.NewRequest("GET", "/api/v3/app/installations/"+inst2.InstallationID, nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404 (body: %s)", rr.Code, rr.Body.String())
		}
	})
}
