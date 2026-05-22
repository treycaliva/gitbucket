package v3

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

// seedLocalRepo creates a tiny bare repo locally with one commit on `main`
// containing README.md + src/main.go.
func seedLocalRepo(t *testing.T, localReposRoot, owner, repo string) string {
	t.Helper()
	bare := filepath.Join(localReposRoot, owner+"_"+repo+".git")
	work := filepath.Join(localReposRoot, "_work_"+owner+"_"+repo)
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
	return bare
}

func TestGetContents(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
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
	_ = seedLocalRepo(t, tmp, owner, repoName)

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	h.LocalReposRoot = tmp // override for test
	RegisterV3Routes(r, h)

	auth := "Bearer " + tok

	t.Run("file: README.md", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName+"/contents/README.md?ref=main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["type"] != "file" || body["encoding"] != "base64" {
			t.Errorf("type/encoding wrong: %+v", body)
		}
		content, _ := body["content"].(string)
		decoded, _ := base64.StdEncoding.DecodeString(content)
		if string(decoded) != "# Hello\n" {
			t.Errorf("decoded = %q, want \"# Hello\\n\"", decoded)
		}
	})

	t.Run("dir: src", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName+"/contents/src?ref=main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var entries []map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &entries)
		if len(entries) != 1 || entries[0]["name"] != "main.go" {
			t.Errorf("dir entries = %+v", entries)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName+"/contents/nope.txt?ref=main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404", rr.Code)
		}
	})
}
