package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestListAndGetPulls(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	tmp := t.TempDir()
	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)
	tok, _ := scen.MintToken(ctx)
	owner := scen.Installation.Account.ID
	repoName := "demo-" + randHex(4)

	if err := db.CreateRepositoryMetadata(ctx, fs, owner, owner, repoName, "", "public"); err != nil {
		t.Fatalf("CreateRepositoryMetadata: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DeleteRepositoryMetadata(context.Background(), fs, owner, repoName)
	})
	seedLocalRepo(t, tmp, owner, repoName)

	// Seed a PR via the db layer (bypasses the existing /api handler's auth).
	pr, err := db.CreatePullRequest(ctx, fs, owner, repoName,
		"Test PR", "From Plan 2 task 6", "main", "main", "alice-uid", "alice")
	if err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort: delete the PR + counter docs; if the PR collection
		// uses repo-scoped IDs we may need to clean by query.
		_ = pr
	})

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	h.LocalReposRoot = tmp
	RegisterV3Routes(r, h)
	auth := "Bearer " + tok

	t.Run("list pulls", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName+"/pulls", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var list []map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &list)
		if len(list) == 0 {
			t.Errorf("expected at least 1 PR in list, got 0")
		}
	})

	t.Run("get pull", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/v3/repos/"+owner+"/"+repoName+"/pulls/"+strconv.Itoa(pr.Number), nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["title"] != "Test PR" {
			t.Errorf("title = %v, want Test PR", body["title"])
		}
		if body["state"] != "open" {
			t.Errorf("state = %v, want open", body["state"])
		}
		head, _ := body["head"].(map[string]interface{})
		if head["ref"] != "main" {
			t.Errorf("head.ref = %v", head["ref"])
		}
	})

	t.Run("get pull → 404", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/v3/repos/"+owner+"/"+repoName+"/pulls/9999", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404", rr.Code)
		}
	})
}

func TestCreateAndUpdatePull(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	tmp := t.TempDir()
	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)
	tok, _ := scen.MintToken(ctx)
	owner := scen.Installation.Account.ID
	repoName := "demo-" + randHex(4)
	if err := db.CreateRepositoryMetadata(ctx, fs, owner, owner, repoName, "", "public"); err != nil {
		t.Fatalf("CreateRepositoryMetadata: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DeleteRepositoryMetadata(context.Background(), fs, owner, repoName)
	})
	seedLocalRepo(t, tmp, owner, repoName)
	seedBranch(t, tmp, owner, repoName, "feature")

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	h.LocalReposRoot = tmp
	RegisterV3Routes(r, h)
	auth := "Bearer " + tok

	var createdNum int
	t.Run("POST creates PR with bot actor", func(t *testing.T) {
		body := bytes.NewBufferString(`{"title":"Plan2 PR","body":"From v3 POST","head":"feature","base":"main"}`)
		req := httptest.NewRequest("POST", "/api/v3/repos/"+owner+"/"+repoName+"/pulls", body)
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("POST code = %d body: %s", rr.Code, rr.Body.String())
		}
		var pr map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &pr)
		if pr["title"] != "Plan2 PR" {
			t.Errorf("title = %v", pr["title"])
		}
		if pr["state"] != "open" {
			t.Errorf("state = %v, want open", pr["state"])
		}
		usr, _ := pr["user"].(map[string]interface{})
		if usr["type"] != "Bot" {
			t.Errorf("user.type = %v, want Bot (actor should be the App's bot user)", usr["type"])
		}
		login, _ := usr["login"].(string)
		if !strings.HasSuffix(login, "[bot]") {
			t.Errorf("user.login = %q, want suffix [bot]", login)
		}
		numF, _ := pr["number"].(float64)
		createdNum = int(numF)
		if createdNum == 0 {
			t.Fatal("PR number not returned")
		}
	})

	t.Run("PATCH title", func(t *testing.T) {
		body := bytes.NewBufferString(`{"title":"Renamed Title"}`)
		req := httptest.NewRequest("PATCH",
			"/api/v3/repos/"+owner+"/"+repoName+"/pulls/"+strconv.Itoa(createdNum), body)
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PATCH code = %d body: %s", rr.Code, rr.Body.String())
		}
		var pr map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &pr)
		if pr["title"] != "Renamed Title" {
			t.Errorf("title after PATCH = %v", pr["title"])
		}
	})

	t.Run("PATCH state=closed", func(t *testing.T) {
		body := bytes.NewBufferString(`{"state":"closed"}`)
		req := httptest.NewRequest("PATCH",
			"/api/v3/repos/"+owner+"/"+repoName+"/pulls/"+strconv.Itoa(createdNum), body)
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PATCH state code = %d body: %s", rr.Code, rr.Body.String())
		}
		var pr map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &pr)
		if pr["state"] != "closed" {
			t.Errorf("state after close = %v", pr["state"])
		}
	})
}

// seedBranch creates an additional branch in the seeded bare repo by pushing
// from a temporary working copy. Reuses the seeded `main` branch.
func seedBranch(t *testing.T, root, owner, repo, branch string) {
	t.Helper()
	bare := root + "/" + owner + "_" + repo + ".git"
	work := root + "/_work_branch_" + owner + "_" + repo + "_" + branch
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	run := func(dir, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run(work, "git", "clone", bare, ".")
	run(work, "git", "checkout", "-b", branch)
	os.WriteFile(work+"/feature-marker.txt", []byte("feature\n"), 0o644)
	run(work, "git", "config", "user.email", "test@test")
	run(work, "git", "config", "user.name", "test")
	run(work, "git", "add", ".")
	run(work, "git", "commit", "-m", "feature commit")
	run(work, "git", "push", "origin", branch)
}
