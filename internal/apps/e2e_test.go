// internal/apps/e2e_test.go
package apps_test

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
	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestTokenMintPopulatesRepositoriesForSelected(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	repoName, err := scen.SeedRepo(ctx)
	if err != nil {
		t.Fatalf("SeedRepo: %v", err)
	}

	r := chi.NewRouter()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	h := apps.NewHandler(fs, scen.Store, jwtV, nil)
	apps.RegisterRoutes(r, h)

	jwtStr := scen.SignJWT(t)
	req := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["repository_selection"] != "selected" {
		t.Errorf("repository_selection = %v, want selected", body["repository_selection"])
	}
	reposRaw, ok := body["repositories"].([]interface{})
	if !ok {
		t.Fatalf("repositories not an array: %v", body["repositories"])
	}
	if len(reposRaw) != 1 {
		t.Fatalf("repositories len = %d, want 1; body=%s", len(reposRaw), rr.Body.String())
	}
	repo, _ := reposRaw[0].(map[string]interface{})
	if repo["name"] != repoName {
		t.Errorf("repos[0].name = %v, want %s", repo["name"], repoName)
	}
}

func TestPlan1AuthPlaneEndToEnd(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	r := chi.NewRouter()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	h := apps.NewHandler(fs, scen.Store, jwtV, nil)
	apps.RegisterRoutes(r, h)

	// Also mount a probe endpoint behind RequireInstallationToken to prove the
	// minted token works end-to-end as bearer credential.
	r.With(apps.RequireInstallationToken(fs)).Get("/__probe", func(w http.ResponseWriter, req *http.Request) {
		ic := apps.InstallationContextFrom(req.Context())
		if ic == nil || ic.AppID == "" {
			http.Error(w, "no installation context", 500)
			return
		}
		apps.WriteJSON(w, 200, map[string]string{"installation_id": ic.InstallationID})
	})

	jwtStr := scen.SignJWT(t)

	// Step 1: Mint a token via the public HTTP endpoint.
	req := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("mint: code = %d body: %s", rr.Code, rr.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &minted)
	tok, _ := minted["token"].(string)
	if !strings.HasPrefix(tok, "ghs_") {
		t.Fatalf("token prefix wrong: %q", tok)
	}
	if minted["expires_at"] == nil {
		t.Fatal("expires_at missing")
	}
	if minted["permissions"] == nil {
		t.Fatal("permissions missing")
	}

	// Step 2: Use the token on a protected route.
	req2 := httptest.NewRequest("GET", "/__probe", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("probe: code = %d body: %s", rr2.Code, rr2.Body.String())
	}
	var probeBody map[string]string
	_ = json.Unmarshal(rr2.Body.Bytes(), &probeBody)
	if probeBody["installation_id"] != scen.Installation.InstallationID {
		t.Errorf("probe returned installation %q, want %q", probeBody["installation_id"], scen.Installation.InstallationID)
	}
}
