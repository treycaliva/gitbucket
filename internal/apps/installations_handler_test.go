package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestCreateUserInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	suffix := randHex(4)
	uid := "ui-uid-" + suffix
	username := "ui-user-" + suffix
	if err := db.RegisterUsername(ctx, fs, uid, username, uid+"@test"); err != nil {
		t.Fatalf("RegisterUsername: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(username).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	store := NewMemorySecretStore()
	botUID, _ := CreateBotUser(ctx, fs, "ui-app-"+suffix, "UI App", "", "pending")
	app, _, err := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug:         "ui-app-" + suffix,
		Name:         "UI App",
		OwnerAccount: AccountRef{ID: "other-uid-" + suffix, Type: AccountTypeUser},
		BotUserID:    botUID,
		WebhookURL:   "https://example.test/hook",
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc("ui-app-" + suffix + "[bot]").Delete(cctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, store, NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	body, _ := json.Marshal(map[string]interface{}{
		"app_id":               app.AppID,
		"repository_selection": "all",
	})
	req := httptest.NewRequest("POST", "/api/v3/user/installations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	installID, _ := resp["id"].(string)
	if installID == "" {
		t.Fatal("response missing id")
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionInstallations).Doc(installID).Delete(context.Background())
	})

	got, _ := GetInstallation(ctx, fs, installID)
	if got == nil {
		t.Fatal("installation not persisted")
	}
	if got.Account.ID != uid {
		t.Errorf("account.id = %q, want %q (the authed user)", got.Account.ID, uid)
	}
}

func TestCreateUserInstallation_AppNotFound(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	uid := "ui-nf-" + randHex(4)
	_ = db.RegisterUsername(ctx, fs, uid, uid, uid+"@test")
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(uid).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, NewMemorySecretStore(), NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	body, _ := json.Marshal(map[string]interface{}{
		"app_id":               "no-such-app",
		"repository_selection": "all",
	})
	req := httptest.NewRequest("POST", "/api/v3/user/installations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rr.Code)
	}
}
