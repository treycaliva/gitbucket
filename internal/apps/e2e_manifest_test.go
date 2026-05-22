package apps_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v4"

	v3 "gitbucket/internal/api/v3"
	"gitbucket/internal/apps"
	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestPlan4FullManifestFlow(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	// Seed the consenting user.
	suffix := randHexExt(4)
	uid := "owner-" + suffix
	username := "owner-" + suffix
	if err := db.RegisterUsername(ctx, fs, uid, username, uid+"@test"); err != nil {
		t.Fatalf("RegisterUsername: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(username).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	store := apps.NewMemorySecretStore()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	appsH := apps.NewHandler(fs, store, jwtV, authH)

	r := chi.NewRouter()
	apps.RegisterRoutes(r, appsH)

	v3H := v3.NewV3Handler(fs, nil, "https://test.gb")
	v3.RegisterV3Routes(r, v3H)

	// --- Hop 2: submit manifest as the logged-in user ---
	manifest := map[string]interface{}{
		"name":            "FakeClaude " + suffix,
		"url":             "https://claude.test",
		"hook_attributes": map[string]interface{}{"url": "https://claude.test/webhook"},
		"redirect_url":    "https://claude.test/setup/callback",
		"default_permissions": map[string]interface{}{
			"contents":      "write",
			"pull_requests": "write",
			"metadata":      "read",
		},
		"default_events": []string{"pull_request", "push"},
	}
	body, _ := json.Marshal(map[string]interface{}{"manifest": manifest})
	req := httptest.NewRequest("POST", "/api/v3/settings/apps/manifest-conversions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("hop2: %d %s", rr.Code, rr.Body.String())
	}
	var hop2 map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &hop2)
	redirectURL, _ := hop2["redirect_url"].(string)
	parts := strings.SplitN(redirectURL, "code=", 2)
	if len(parts) != 2 {
		t.Fatalf("no code in redirect_url: %s", redirectURL)
	}
	code := parts[1]

	// --- Hop 3: agent exchanges the code ---
	rr3 := httptest.NewRecorder()
	r.ServeHTTP(rr3, httptest.NewRequest("POST", "/api/v3/app-manifests/"+code+"/conversions", nil))
	if rr3.Code != http.StatusOK {
		t.Fatalf("hop3: %d %s", rr3.Code, rr3.Body.String())
	}
	var bundle map[string]interface{}
	_ = json.Unmarshal(rr3.Body.Bytes(), &bundle)
	pemStr, _ := bundle["pem"].(string)
	appID, _ := bundle["id"].(string)
	slug, _ := bundle["slug"].(string)
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(apps.CollectionApps).Doc(appID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		docs, _ := fs.Collection(apps.CollectionUsers).Where("username", "==", slug+"[bot]").Documents(cctx).GetAll()
		for _, d := range docs {
			_, _ = d.Ref.Delete(cctx)
		}
	})

	// --- Install the App on the same user's account ---
	installBody, _ := json.Marshal(map[string]interface{}{
		"app_id":               appID,
		"repository_selection": "all",
	})
	installReq := httptest.NewRequest("POST", "/api/v3/user/installations", bytes.NewReader(installBody))
	installReq.Header.Set("Authorization", "Bearer mock_"+uid)
	installRR := httptest.NewRecorder()
	r.ServeHTTP(installRR, installReq)
	if installRR.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", installRR.Code, installRR.Body.String())
	}
	var installResp map[string]interface{}
	_ = json.Unmarshal(installRR.Body.Bytes(), &installResp)
	installationID, _ := installResp["id"].(string)
	t.Cleanup(func() {
		_, _ = fs.Collection(apps.CollectionInstallations).Doc(installationID).Delete(context.Background())
	})

	// --- Use the App PEM to sign a JWT, then mint an installation token ---
	block, _ := pem.Decode([]byte(pemStr))
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse pkcs1: %v", err)
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": appID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}
	jwtStr, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(priv)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	mintReq := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+installationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	mintReq.Header.Set("Authorization", "Bearer "+jwtStr)
	mintRR := httptest.NewRecorder()
	r.ServeHTTP(mintRR, mintReq)
	if mintRR.Code != http.StatusCreated {
		t.Fatalf("mint: %d %s", mintRR.Code, mintRR.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(mintRR.Body.Bytes(), &minted)
	tok, _ := minted["token"].(string)
	if !strings.HasPrefix(tok, "ghs_") {
		t.Fatalf("bad token: %q", tok)
	}

	// --- Use the token on an installation-authed endpoint ---
	probeReq := httptest.NewRequest("GET", "/api/v3/_ping", nil)
	probeReq.Header.Set("Authorization", "Bearer "+tok)
	probeRR := httptest.NewRecorder()
	r.ServeHTTP(probeRR, probeReq)
	if probeRR.Code != http.StatusOK {
		t.Fatalf("ping: %d %s", probeRR.Code, probeRR.Body.String())
	}
}

// randHexExt is a small test helper (package-level to avoid conflict with the
// internal randHex in package apps).
func randHexExt(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
