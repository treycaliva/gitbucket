package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestBrowseAPIs(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping Browse API integration tests: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer client.Close()

	// Setup local repos root directory for the test
	tempReposRoot, err := os.MkdirTemp("", "gitbucket-browse-repos-*")
	if err != nil {
		t.Fatalf("failed to create temp repos root: %v", err)
	}
	defer os.RemoveAll(tempReposRoot)

	authHandler := auth.NewAuthHandler(true, nil, client)
	apiHandler := NewAPIHandler(client, nil, "", tempReposRoot, authHandler)

	r := chi.NewRouter()
	apiHandler.RegisterRoutes(r)

	suffix := randSuffix()
	uid := "browse-uid-" + suffix
	username := "browse-user-" + suffix

	// Register username
	regBody, _ := json.Marshal(map[string]string{"username": username})
	req := httptest.NewRequest("POST", "/api/user/username", bytes.NewBuffer(regBody))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("failed to register user in test: %d (body: %s)", rr.Code, rr.Body.String())
	}

	// Create a public repository metadata in Firestore
	repoName := "testrepo-" + suffix
	err = db.CreateRepositoryMetadata(ctx, client, uid, username, repoName, "Browse Test Repo", "public")
	if err != nil {
		t.Fatalf("failed to create repository metadata: %v", err)
	}

	// Create a private repository metadata in Firestore
	privRepoName := "privrepo-" + suffix
	err = db.CreateRepositoryMetadata(ctx, client, uid, username, privRepoName, "Browse Private Test Repo", "private")
	if err != nil {
		t.Fatalf("failed to create private repository metadata: %v", err)
	}

	// Let's build a real local git repository, commit some changes, and clone/copy it as a bare repo
	workDir, err := os.MkdirTemp("", "gitbucket-work-repo-*")
	if err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}
	defer os.RemoveAll(workDir)

	// Run git commands in workDir to populate repository
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git command failed: git %s: %v: %s", strings.Join(args, " "), err, stderr.String())
		}
	}

	runGit("init")
	runGit("config", "user.name", "Test Author")
	runGit("config", "user.email", "test@example.com")

	// Create README.md
	err = os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Hello GitBucket\nThis is a test."), 0644)
	if err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}

	// Create a directory with a source file
	srcDir := filepath.Join(workDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}
	err = os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main\n\nfunc main() {}"), 0644)
	if err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	// Add an image format mock
	err = os.WriteFile(filepath.Join(workDir, "logo.png"), []byte("PNG_MOCK_DATA"), 0644)
	if err != nil {
		t.Fatalf("failed to write logo.png: %v", err)
	}

	runGit("add", ".")
	runGit("commit", "-m", "Initial commit")

	// Commit 2: Edit README.md
	err = os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Hello GitBucket\nThis is a test.\nSecond line."), 0644)
	if err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	runGit("commit", "-am", "Second commit")

	// Clone bare repository to tempReposRoot
	destPath := filepath.Join(tempReposRoot, strings.ToLower(username), repoName+".git")
	err = os.MkdirAll(filepath.Dir(destPath), 0755)
	if err != nil {
		t.Fatalf("failed to create destination parent: %v", err)
	}
	cmdClone := exec.Command("git", "clone", "--bare", workDir, destPath)
	if err := cmdClone.Run(); err != nil {
		t.Fatalf("failed to clone bare repository: %v", err)
	}

	// Clone the same to private repo path
	privDestPath := filepath.Join(tempReposRoot, strings.ToLower(username), privRepoName+".git")
	cmdClonePriv := exec.Command("git", "clone", "--bare", workDir, privDestPath)
	if err := cmdClonePriv.Run(); err != nil {
		t.Fatalf("failed to clone bare private repository: %v", err)
	}

	// Let's run tests for endpoints:

	// 1. GET /api/repos/{owner}/{repo}/commits/{branch}
	t.Run("GetCommitHistory_Public", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/commits/main", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
		}
		var commits []CommitInfo
		if err := json.NewDecoder(rr.Body).Decode(&commits); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(commits) != 2 {
			t.Errorf("expected 2 commits, got %d", len(commits))
		}
		if commits[0].Message != "Second commit" {
			t.Errorf("expected latest commit to be 'Second commit', got %q", commits[0].Message)
		}
	})

	t.Run("GetCommitHistory_Private_Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+privRepoName+"/commits/main", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for private repo without auth, got %d", rr.Code)
		}
	})

	t.Run("GetCommitHistory_Private_Forbidden", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+privRepoName+"/commits/main", nil)
		req.Header.Set("Authorization", "Bearer mock_otheruser")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for private repo with wrong auth, got %d", rr.Code)
		}
	})

	t.Run("GetCommitHistory_Private_Authorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+privRepoName+"/commits/main", nil)
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
	})

	// 2. GET /api/repos/{owner}/{repo}/commit/{sha}
	t.Run("GetCommitDiff", func(t *testing.T) {
		// First get the SHA from commits
		reqCommits := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/commits/main", nil)
		rrCommits := httptest.NewRecorder()
		r.ServeHTTP(rrCommits, reqCommits)
		var commits []CommitInfo
		_ = json.NewDecoder(rrCommits.Body).Decode(&commits)
		sha := commits[0].SHA

		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/commit/"+sha, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
		var diffResp map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&diffResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !strings.Contains(diffResp["rawDiff"], "Second commit") {
			t.Errorf("expected diff to contain 'Second commit', got %q", diffResp["rawDiff"])
		}
	})

	// 3. GET /api/repos/{owner}/{repo}/tree/{branch}
	t.Run("GetTree_Root", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/tree/main", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
		var tree []TreeEntry
		if err := json.NewDecoder(rr.Body).Decode(&tree); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		// Expecting README.md, logo.png, src/
		if len(tree) != 3 {
			t.Errorf("expected 3 entries in tree, got %d", len(tree))
		}

		hasREADME := false
		hasSrc := false
		for _, entry := range tree {
			if entry.Path == "README.md" && entry.Type == "blob" {
				hasREADME = true
			}
			if entry.Path == "src" && entry.Type == "tree" {
				hasSrc = true
			}
		}
		if !hasREADME || !hasSrc {
			t.Errorf("expected README.md and src in tree, got %+v", tree)
		}
	})

	// 4. GET /api/repos/{owner}/{repo}/tree/{branch}/*
	t.Run("GetTree_Subpath", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/tree/main/src", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
		var tree []TreeEntry
		if err := json.NewDecoder(rr.Body).Decode(&tree); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		// Expecting main.go
		if len(tree) != 1 {
			t.Errorf("expected 1 entry in tree/src, got %d", len(tree))
		}
		if tree[0].Name != "main.go" || tree[0].Path != "src/main.go" {
			t.Errorf("expected src/main.go, got %+v", tree[0])
		}
	})

	t.Run("GetTree_InvalidPath", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/tree/main/nonexistent", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK for invalid path tree, got %d", rr.Code)
		}
		var tree []TreeEntry
		_ = json.NewDecoder(rr.Body).Decode(&tree)
		if len(tree) != 0 {
			t.Errorf("expected empty array for invalid path tree, got %d entries", len(tree))
		}
	})

	// 5. GET /api/repos/{owner}/{repo}/blob/{branch}/*
	t.Run("GetBlob_Text", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/blob/main/README.md", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
		contentType := rr.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/plain") {
			t.Errorf("expected text/plain content type, got %q", contentType)
		}
		body := rr.Body.String()
		if !strings.Contains(body, "Hello GitBucket") {
			t.Errorf("expected README content, got %q", body)
		}
	})

	t.Run("GetBlob_Image", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/blob/main/logo.png", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
		contentType := rr.Header().Get("Content-Type")
		if contentType != "image/png" {
			t.Errorf("expected image/png content type, got %q", contentType)
		}
		body := rr.Body.String()
		if body != "PNG_MOCK_DATA" {
			t.Errorf("expected image content, got %q", body)
		}
	})

	t.Run("GetBlob_NotFound", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/blob/main/nonexistent.txt", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found, got %d", rr.Code)
		}
	})

	// 6. GET /api/config
	t.Run("GetConfig", func(t *testing.T) {
		os.Setenv("DEV_MODE", "true")
		os.Setenv("FIREBASE_API_KEY", "mock-api-key")
		defer func() {
			os.Unsetenv("DEV_MODE")
			os.Unsetenv("FIREBASE_API_KEY")
		}()

		req := httptest.NewRequest("GET", "/api/config", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
		var configResp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&configResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if configResp["devMode"] != true {
			t.Errorf("expected devMode true, got %v", configResp["devMode"])
		}
		firebaseMap, ok := configResp["firebase"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected firebase config map, got %v", configResp["firebase"])
		}
		if firebaseMap["apiKey"] != "mock-api-key" {
			t.Errorf("expected apiKey 'mock-api-key', got %v", firebaseMap["apiKey"])
		}
	})
}
