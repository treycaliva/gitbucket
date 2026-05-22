package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestGetRefAndGetTree(t *testing.T) {
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

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	h.LocalReposRoot = tmp
	RegisterV3Routes(r, h)
	auth := "Bearer " + tok

	t.Run("GET ref heads/main", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/v3/repos/"+owner+"/"+repoName+"/git/ref/heads/main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["ref"] != "refs/heads/main" {
			t.Errorf("ref = %v", body["ref"])
		}
		obj, _ := body["object"].(map[string]interface{})
		sha, _ := obj["sha"].(string)
		if len(sha) != 40 {
			t.Errorf("object.sha length = %d (want 40 hex chars), value=%q", len(sha), sha)
		}
	})

	t.Run("GET ref → 404 on missing ref", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/v3/repos/"+owner+"/"+repoName+"/git/ref/heads/no-such-branch", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404", rr.Code)
		}
	})

	t.Run("GET trees", func(t *testing.T) {
		// First resolve main's commit SHA via git ref.
		req := httptest.NewRequest("GET",
			"/api/v3/repos/"+owner+"/"+repoName+"/git/ref/heads/main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		var refBody map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &refBody)
		obj := refBody["object"].(map[string]interface{})
		commitSHA := obj["sha"].(string)

		// GET the tree by the commit SHA.
		req = httptest.NewRequest("GET",
			"/api/v3/repos/"+owner+"/"+repoName+"/git/trees/"+commitSHA, nil)
		req.Header.Set("Authorization", auth)
		rr = httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var treeBody map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &treeBody)
		entries, _ := treeBody["tree"].([]interface{})
		if len(entries) < 2 {
			t.Errorf("expected at least 2 entries (README.md + src), got %d", len(entries))
		}
	})
}
