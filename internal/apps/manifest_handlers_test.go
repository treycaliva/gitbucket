package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestManifestConversionEndpoint(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	authH := auth.NewAuthHandler(true, nil, fs) // DevMode = true; mock_<uid> Bearer tokens work
	store := NewMemorySecretStore()
	jwt := NewJWTVerifier(fs, 60*time.Second)
	h := NewHandler(fs, store, jwt, authH)

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	suffix := randHex(4)
	uid := "manifest-uid-" + suffix
	username := "manifest-user-" + suffix
	// Pre-register the username so the manifest handler can look up the owner.
	if err := db.RegisterUsername(ctx, fs, uid, username, uid+"@test"); err != nil {
		t.Fatalf("RegisterUsername: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(username).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	manifest := map[string]interface{}{
		"name":            "Test Manifest App " + suffix,
		"url":             "https://example.test",
		"hook_attributes": map[string]interface{}{"url": "https://example.test/webhook"},
		"redirect_url":    "https://example.test/setup/callback",
		"public":          false,
		"default_permissions": map[string]interface{}{
			"contents":      "write",
			"issues":        "write",
			"pull_requests": "write",
			"metadata":      "read",
		},
		"default_events": []string{"issues", "issue_comment", "pull_request"},
	}
	body, _ := json.Marshal(map[string]interface{}{"manifest": manifest})
	req := httptest.NewRequest("POST", "/api/v3/settings/apps/manifest-conversions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	redirectURL, _ := resp["redirect_url"].(string)
	if !strings.HasPrefix(redirectURL, "https://example.test/setup/callback?code=") {
		t.Errorf("redirect_url = %q (expected to start with the manifest's redirect_url + ?code=)", redirectURL)
	}

	// Extract the code and exchange it via hop 3.
	code := strings.TrimPrefix(redirectURL, "https://example.test/setup/callback?code=")
	if code == "" {
		t.Fatal("could not extract code")
	}

	exchangeReq := httptest.NewRequest("POST", "/api/v3/app-manifests/"+code+"/conversions", nil)
	exchangeRR := httptest.NewRecorder()
	r.ServeHTTP(exchangeRR, exchangeReq)
	if exchangeRR.Code != http.StatusOK {
		t.Fatalf("exchange code = %d body: %s", exchangeRR.Code, exchangeRR.Body.String())
	}
	var bundle map[string]interface{}
	_ = json.Unmarshal(exchangeRR.Body.Bytes(), &bundle)
	for _, key := range []string{"id", "slug", "name", "owner", "client_id", "client_secret", "webhook_secret", "pem"} {
		if _, ok := bundle[key]; !ok {
			t.Errorf("bundle missing key %q", key)
		}
	}
	if pem, _ := bundle["pem"].(string); !strings.Contains(pem, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("pem not a PKCS1 RSA private key, got: %q", pem)
	}

	// Cleanup the App + bot user.
	appID, _ := bundle["id"].(string)
	slug, _ := bundle["slug"].(string)
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionApps).Doc(appID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		docs, _ := fs.Collection(CollectionUsers).Where("username", "==", slug+"[bot]").Documents(cctx).GetAll()
		for _, d := range docs {
			_, _ = d.Ref.Delete(cctx)
		}
	})

	// Second exchange must fail (single-use).
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, httptest.NewRequest("POST", "/api/v3/app-manifests/"+code+"/conversions", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("second exchange code = %d, want 404", rr2.Code)
	}
}

func TestManifestConversionRejectsUnauthenticated(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, NewMemorySecretStore(), NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	req := httptest.NewRequest("POST", "/api/v3/settings/apps/manifest-conversions",
		strings.NewReader(`{"manifest":{}}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestListMyAppsAndInstallations(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	suffix := randHex(4)
	uid := "list-uid-" + suffix
	username := "list-user-" + suffix
	if err := db.RegisterUsername(ctx, fs, uid, username, uid+"@test"); err != nil {
		t.Fatalf("RegisterUsername: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(username).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	// Seed an App owned by this user.
	store := NewMemorySecretStore()
	slug := "list-app-" + suffix
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, _, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug:         slug,
		Name:         "List App",
		OwnerAccount: AccountRef{ID: uid, Type: AccountTypeUser},
		BotUserID:    botUID,
		WebhookURL:   "https://example.test/hook",
	})
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
	})

	// Seed an installation on this user's account using a DIFFERENT App
	// (so listMyApps and listMyInstallations test different things).
	otherSlug := "other-app-" + suffix
	otherBot, _ := CreateBotUser(ctx, fs, otherSlug, otherSlug, "", "pending")
	otherApp, _, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug:         otherSlug,
		Name:         "Other App",
		OwnerAccount: AccountRef{ID: "someone-else-" + suffix, Type: AccountTypeUser},
		BotUserID:    otherBot,
		WebhookURL:   "https://example.test/other",
	})
	inst, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID:               otherApp.AppID,
		Account:             AccountRef{ID: uid, Type: AccountTypeUser},
		RepositorySelection: "all",
	})
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(cctx)
		_, _ = fs.Collection(CollectionApps).Doc(otherApp.AppID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(otherSlug + "[bot]").Delete(cctx)
		_, _ = fs.Collection(CollectionUsers).Doc(otherBot).Delete(cctx)
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, store, NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	t.Run("list my apps", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/user/apps", nil)
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var list []map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &list)
		var foundOwned bool
		for _, item := range list {
			if item["slug"] == slug {
				foundOwned = true
			}
		}
		if !foundOwned {
			t.Errorf("expected owned app %q in list, got %+v", slug, list)
		}
	})

	t.Run("list my installations", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/user/installations", nil)
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var list []map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &list)
		var foundInstall bool
		for _, item := range list {
			if item["app_id"] == otherApp.AppID {
				foundInstall = true
				if item["app_name"] != "Other App" {
					t.Errorf("app_name = %v, want Other App", item["app_name"])
				}
			}
		}
		if !foundInstall {
			t.Errorf("expected installation of %q in list, got %+v", otherApp.AppID, list)
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/user/apps", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("code = %d, want 401", rr.Code)
		}
	})
}

func TestGetAppPublic(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	suffix := randHex(4)
	slug := "pubapp-" + suffix
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	store := NewMemorySecretStore()
	app, _, err := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug:         slug,
		Name:         "Public App",
		OwnerAccount: AccountRef{ID: "owner-" + suffix, Type: AccountTypeUser},
		BotUserID:    botUID,
		WebhookURL:   "https://example.test/hook",
		DefaultPermissions: Permissions{"issues": PermWrite, "metadata": PermRead},
		DefaultEvents:      []string{"issues", "pull_request"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, store, NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	// Found.
	req := httptest.NewRequest("GET", "/api/v3/apps/"+slug+"/public", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["slug"] != slug || body["name"] != "Public App" {
		t.Errorf("body = %+v", body)
	}
	if _, ok := body["permissions"].(map[string]interface{}); !ok {
		t.Errorf("permissions field missing or wrong type")
	}

	// Not found.
	req = httptest.NewRequest("GET", "/api/v3/apps/no-such-slug/public", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rr.Code)
	}
}
