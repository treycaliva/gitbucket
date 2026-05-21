package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
