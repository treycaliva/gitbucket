// internal/apps/middleware_test.go
package apps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"gitbucket/internal/db"
)

func TestRequireInstallationTokenMiddleware(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "mw-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, _, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite},
	})
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
	})
	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         Permissions{"issues": PermWrite},
		Events:              []string{"issues"},
	})
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(cctx)
	})

	out, _ := MintInstallationToken(ctx, fs, inst, MintRequest{})

	makeHandler := func(scope string, need PermissionLevel) http.Handler {
		return RequireInstallationToken(fs)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := RequirePerm(r.Context(), scope, need); err != nil {
				WriteError(w, err)
				return
			}
			icx := InstallationContextFrom(r.Context())
			if icx == nil || icx.InstallationID != inst.InstallationID {
				t.Error("missing or wrong installation context")
				http.Error(w, "bad ctx", 500)
				return
			}
			w.WriteHeader(200)
		}))
	}

	t.Run("missing token → 401", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		makeHandler("issues", PermRead).ServeHTTP(rr, req)
		if rr.Code != 401 {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("valid bearer + sufficient perm → 200", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		req.Header.Set("Authorization", "Bearer "+out.Plaintext)
		makeHandler("issues", PermRead).ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("code = %d (body: %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("HTTP Basic x-access-token also works", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		req.SetBasicAuth("x-access-token", out.Plaintext)
		makeHandler("issues", PermRead).ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("code = %d (body: %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("insufficient perm → 403", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		req.Header.Set("Authorization", "Bearer "+out.Plaintext)
		makeHandler("contents", PermWrite).ServeHTTP(rr, req)
		if rr.Code != 403 {
			t.Errorf("code = %d (body: %s)", rr.Code, rr.Body.String())
		}
	})
}
