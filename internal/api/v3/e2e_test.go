package v3_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	v3 "gitbucket/internal/api/v3"
	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

// seedBareRepoWithFeatureBranch creates a bare repo with main + a feature
// branch, both with at least one commit. Self-contained (does not depend on
// helpers in other _test.go files in package v3).
func seedBareRepoWithFeatureBranch(t *testing.T, root, owner, repo string) {
	t.Helper()
	bare := filepath.Join(root, owner+"_"+repo+".git")
	work := filepath.Join(root, "_e2e_work_"+owner+"_"+repo)
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	run := func(dir, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run(bare, "git", "init", "--bare", "--initial-branch=main")
	run(work, "git", "init", "--initial-branch=main")
	run(work, "git", "config", "user.email", "test@test")
	run(work, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# Hello\n"), 0o644)
	os.MkdirAll(filepath.Join(work, "src"), 0o755)
	os.WriteFile(filepath.Join(work, "src", "main.go"), []byte("package main\n"), 0o644)
	run(work, "git", "add", ".")
	run(work, "git", "commit", "-m", "init")
	run(work, "git", "remote", "add", "origin", bare)
	run(work, "git", "push", "origin", "main")

	// Create a feature branch with an additional commit.
	run(work, "git", "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o644)
	run(work, "git", "add", ".")
	run(work, "git", "commit", "-m", "feature")
	run(work, "git", "push", "origin", "feature")
}

func TestPlan2FakeAppWalkthrough(t *testing.T) {
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
	tmp := t.TempDir()
	owner := scen.Installation.Account.ID
	seedBareRepoWithFeatureBranch(t, tmp, owner, repoName)

	// Wire up the full router: both apps (JWT) and v3 (installation token)
	// endpoints share the same chi.Router.
	r := chi.NewRouter()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	appsH := apps.NewHandler(fs, scen.Store, jwtV, nil)
	apps.RegisterRoutes(r, appsH)

	v3H := v3.NewV3Handler(fs, nil, "https://test.gitbucket.local")
	v3H.LocalReposRoot = tmp
	v3.RegisterV3Routes(r, v3H)

	// --- Step 1: mint installation token via JWT-authed endpoint ---
	jwtStr := scen.SignJWT(t)
	req := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("mint: code=%d body=%s", rr.Code, rr.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &minted)
	tok, _ := minted["token"].(string)
	if tok == "" {
		t.Fatalf("token missing in response: %+v", minted)
	}
	repos, _ := minted["repositories"].([]interface{})
	if len(repos) != 1 {
		t.Fatalf("repositories[] len = %d, want 1 (Plan 1 fixup)", len(repos))
	}

	auth := "Bearer " + tok

	// --- Step 2: GET the repo ---
	req = httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName, nil)
	req.Header.Set("Authorization", auth)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get repo: code=%d body=%s", rr.Code, rr.Body.String())
	}
	var repoBody map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &repoBody)
	if repoBody["name"] != repoName {
		t.Errorf("repo.name = %v", repoBody["name"])
	}

	// --- Step 3: GET README.md contents ---
	req = httptest.NewRequest("GET",
		"/api/v3/repos/"+owner+"/"+repoName+"/contents/README.md?ref=main", nil)
	req.Header.Set("Authorization", auth)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get contents: code=%d body=%s", rr.Code, rr.Body.String())
	}
	var contentsBody map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &contentsBody)
	if contentsBody["type"] != "file" {
		t.Errorf("contents.type = %v", contentsBody["type"])
	}

	// --- Step 4: GET pulls list (empty initially) ---
	req = httptest.NewRequest("GET",
		"/api/v3/repos/"+owner+"/"+repoName+"/pulls", nil)
	req.Header.Set("Authorization", auth)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list pulls: code=%d body=%s", rr.Code, rr.Body.String())
	}

	// --- Step 5: POST a PR ---
	body := bytes.NewBufferString(`{"title":"E2E PR","body":"From Plan 2 e2e","head":"feature","base":"main"}`)
	req = httptest.NewRequest("POST",
		"/api/v3/repos/"+owner+"/"+repoName+"/pulls", body)
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create pull: code=%d body=%s", rr.Code, rr.Body.String())
	}
	var pr map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &pr)
	usr, _ := pr["user"].(map[string]interface{})
	if usr["type"] != "Bot" {
		t.Errorf("created PR user.type = %v, want Bot (bot actor)", usr["type"])
	}
	if pr["title"] != "E2E PR" {
		t.Errorf("PR title = %v", pr["title"])
	}
}
