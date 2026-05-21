package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
	gitpkg "gitbucket/internal/git"
	"gitbucket/internal/sync"
)

func TestPullRequestAPIs(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping Pull Request API integration tests: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer client.Close()

	// Setup local repos root directory for the test
	tempReposRoot, err := os.MkdirTemp("", "gitbucket-pr-repos-*")
	if err != nil {
		t.Fatalf("failed to create temp repos root: %v", err)
	}
	defer os.RemoveAll(tempReposRoot)

	authHandler := auth.NewAuthHandler(true, nil, client)
	apiHandler := NewAPIHandler(client, nil, "", tempReposRoot, authHandler, sync.NewKMSClient(nil, ""))

	r := chi.NewRouter()
	apiHandler.RegisterRoutes(r)

	suffix := randSuffix()
	uid := "pr-uid-" + suffix
	username := "pr-user-" + suffix

	// Register username
	regBody, _ := json.Marshal(map[string]string{"username": username})
	req := httptest.NewRequest("POST", "/api/user/username", bytes.NewBuffer(regBody))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("failed to register user in test: %d (body: %s)", rr.Code, rr.Body.String())
	}

	// Create repository metadata in Firestore
	repoName := "prrepo-" + suffix
	err = db.CreateRepositoryMetadata(ctx, client, uid, username, repoName, "Pull Request Test Repo", "public")
	if err != nil {
		t.Fatalf("failed to create repository metadata: %v", err)
	}

	// Let's build a real local git repository with multiple branches/commits
	workDir, err := os.MkdirTemp("", "gitbucket-work-pr-*")
	if err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}
	defer os.RemoveAll(workDir)

	runGitCmd := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git command failed: git %s: %v: %s", strings.Join(args, " "), err, stderr.String())
		}
	}

	runGitCmd("init")
	runGitCmd("config", "user.name", "PR Author")
	runGitCmd("config", "user.email", "prauthor@example.com")

	// 1. Initial commit on main
	err = os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("Base content on main branch.\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write file.txt: %v", err)
	}
	runGitCmd("add", "file.txt")
	runGitCmd("commit", "-m", "Initial base commit")

	// Rename branch to main if needed
	_ = exec.Command("git", "-C", workDir, "branch", "-M", "main").Run()

	// 2. Create feature-1 branch and commit a change
	runGitCmd("checkout", "-b", "feature-1")
	err = os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("Base content on main branch.\nAdd feature-1 content.\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write file.txt in feature-1: %v", err)
	}
	runGitCmd("commit", "-am", "Implement feature-1")

	// 3. Go back to main, create feature-2 branch and commit another change
	runGitCmd("checkout", "main")
	runGitCmd("checkout", "-b", "feature-2")
	err = os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("Base content on main branch.\nAdd feature-2 content.\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write file.txt in feature-2: %v", err)
	}
	runGitCmd("commit", "-am", "Implement feature-2")

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

	// Update branch metadata in Firestore
	err = db.UpdateRepositoryMetadata(ctx, client, username, repoName, map[string]interface{}{
		"branches": []string{"main", "feature-1", "feature-2"},
	})
	if err != nil {
		t.Fatalf("failed to update repository branches in Firestore: %v", err)
	}

	// Let's run PR API Tests:

	// 1. Create Pull Request (POST /api/repos/{owner}/{repo}/pulls)
	var pr1Number, pr2Number int

	t.Run("CreatePullRequest_Success", func(t *testing.T) {
		prBody, _ := json.Marshal(map[string]string{
			"title":        "PR for feature 1",
			"description":  "Merge feature-1 changes into main",
			"sourceBranch": "feature-1",
			"targetBranch": "main",
		})
		req := httptest.NewRequest("POST", "/api/repos/"+username+"/"+repoName+"/pulls", bytes.NewBuffer(prBody))
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d (body: %s)", rr.Code, rr.Body.String())
		}

		var resp db.PullRequest
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Number != 1 {
			t.Errorf("expected PR number 1, got %d", resp.Number)
		}
		if resp.Status != "open" {
			t.Errorf("expected PR status 'open', got %s", resp.Status)
		}
		pr1Number = resp.Number
	})

	t.Run("CreateSecondPullRequest_Success", func(t *testing.T) {
		prBody, _ := json.Marshal(map[string]string{
			"title":        "PR for feature 2",
			"description":  "Merge feature-2 changes into main",
			"sourceBranch": "feature-2",
			"targetBranch": "main",
		})
		req := httptest.NewRequest("POST", "/api/repos/"+username+"/"+repoName+"/pulls", bytes.NewBuffer(prBody))
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d (body: %s)", rr.Code, rr.Body.String())
		}

		var resp db.PullRequest
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Number != 2 {
			t.Errorf("expected PR number 2, got %d", resp.Number)
		}
		pr2Number = resp.Number
	})

	t.Run("CreatePullRequest_BranchNotExist", func(t *testing.T) {
		prBody, _ := json.Marshal(map[string]string{
			"title":        "Invalid PR",
			"description":  "Branch does not exist",
			"sourceBranch": "non-existent-branch",
			"targetBranch": "main",
		})
		req := httptest.NewRequest("POST", "/api/repos/"+username+"/"+repoName+"/pulls", bytes.NewBuffer(prBody))
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d (body: %s)", rr.Code, rr.Body.String())
		}
	})

	// 2. List Pull Requests (GET /api/repos/{owner}/{repo}/pulls)
	t.Run("ListPullRequests", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/repos/"+username+"/"+repoName+"/pulls", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var list []db.PullRequest
		if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 PRs in list, got %d", len(list))
		}
	})

	// 3. Get single Pull Request details (GET /api/repos/{owner}/{repo}/pulls/{number})
	t.Run("GetPullRequest_Details", func(t *testing.T) {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/repos/%s/%s/pulls/%d", username, repoName, pr1Number), nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp db.PullRequest
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Title != "PR for feature 1" {
			t.Errorf("expected title 'PR for feature 1', got %q", resp.Title)
		}
	})

	// 4. Get PR commits (GET /api/repos/{owner}/{repo}/pulls/{number}/commits)
	t.Run("GetPullRequest_Commits", func(t *testing.T) {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/commits", username, repoName, pr1Number), nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
		}

		var commits []CommitInfo
		if err := json.NewDecoder(rr.Body).Decode(&commits); err != nil {
			t.Fatalf("failed to decode commits: %v", err)
		}
		if len(commits) != 1 {
			t.Fatalf("expected 1 commit between main and feature-1, got %d", len(commits))
		}
		if commits[0].Message != "Implement feature-1" {
			t.Errorf("expected commit message 'Implement feature-1', got %q", commits[0].Message)
		}
	})

	// 5. Get PR diff (GET /api/repos/{owner}/{repo}/pulls/{number}/diff)
	t.Run("GetPullRequest_Diff", func(t *testing.T) {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/diff", username, repoName, pr1Number), nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var diffResp map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&diffResp); err != nil {
			t.Fatalf("failed to decode diff: %v", err)
		}
		if !strings.Contains(diffResp["rawDiff"], "Add feature-1 content.") {
			t.Errorf("expected diff to contain 'Add feature-1 content.', got %q", diffResp["rawDiff"])
		}
	})

	// 6. Close Pull Request (POST /api/repos/{owner}/{repo}/pulls/{number}/close)
	t.Run("ClosePullRequest", func(t *testing.T) {
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/close", username, repoName, pr2Number), nil)
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp["status"] != "closed" {
			t.Errorf("expected status 'closed', got %v", resp["status"])
		}

		// Verify in DB
		pr, _ := db.GetPullRequest(ctx, client, username, repoName, pr2Number)
		if pr.Status != "closed" {
			t.Errorf("expected DB status to be 'closed', got %s", pr.Status)
		}
	})

	// 7. Merge Pull Request (POST /api/repos/{owner}/{repo}/pulls/{number}/merge)
	t.Run("MergePullRequest_Success", func(t *testing.T) {
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/merge", username, repoName, pr1Number), nil)
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp["status"] != "merged" {
			t.Errorf("expected status 'merged', got %v", resp["status"])
		}

		// Verify PR status in DB
		pr, _ := db.GetPullRequest(ctx, client, username, repoName, pr1Number)
		if pr.Status != "merged" {
			t.Errorf("expected DB status to be 'merged', got %s", pr.Status)
		}

		// Verify target branch 'main' in the bare repo now has the feature-1 commit message and contents
		cmdLog := exec.Command("git", "--git-dir", destPath, "log", "main", "--format=%s")
		var out bytes.Buffer
		cmdLog.Stdout = &out
		if err := cmdLog.Run(); err != nil {
			t.Fatalf("failed to check log of merged branch in bare repo: %v", err)
		}
		logContent := out.String()
		if !strings.Contains(logContent, "Merge pull request #1") {
			t.Errorf("expected log to contain merge commit message, got %q", logContent)
		}
		if !strings.Contains(logContent, "Implement feature-1") {
			t.Errorf("expected log to contain 'Implement feature-1', got %q", logContent)
		}
	})
}

// setupMergeGateFixture builds an isolated repo (registered in Firestore +
// materialized on disk) with a `main` branch and a `feat` branch carrying a
// single file change. It returns the chi router, the username/repoName/PR
// number, the uid, and a path to the bare repo. Each call uses a unique
// suffix so tests can run against the same emulator without stepping on
// each other.
func setupMergeGateFixture(t *testing.T, ctx context.Context, dbClient *firestore.Client) (
	router *chi.Mux, owner, repoName string, uid string, prNumber int, barePath string,
) {
	t.Helper()

	tempReposRoot, err := os.MkdirTemp("", "gitbucket-mergegate-repos-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempReposRoot) })

	authH := auth.NewAuthHandler(true, nil, dbClient)
	apiH := NewAPIHandler(dbClient, nil, "", tempReposRoot, authH, sync.NewKMSClient(nil, ""))
	r := chi.NewRouter()
	apiH.RegisterRoutes(r)

	suffix := randSuffix()
	uid = "mg-uid-" + suffix
	owner = "mg-user-" + suffix
	repoName = "mg-repo-" + suffix

	// Register username
	regBody, _ := json.Marshal(map[string]string{"username": owner})
	req := httptest.NewRequest("POST", "/api/user/username", bytes.NewBuffer(regBody))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("register username: %d %s", rr.Code, rr.Body.String())
	}

	if err := db.CreateRepositoryMetadata(ctx, dbClient, uid, owner, repoName, "", "public"); err != nil {
		t.Fatalf("create repo meta: %v", err)
	}

	// Build a working repo with main + feat branches.
	workDir, err := os.MkdirTemp("", "gitbucket-mergegate-work-*")
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })

	runG := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, stderr.String())
		}
	}
	runG("init")
	runG("config", "user.name", "T")
	runG("config", "user.email", "t@example.com")
	_ = os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("a\n"), 0644)
	runG("add", ".")
	runG("commit", "-m", "init")
	_ = exec.Command("git", "-C", workDir, "branch", "-M", "main").Run()
	runG("checkout", "-b", "feat")
	_ = os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("a\nb\n"), 0644)
	runG("commit", "-am", "feat")

	barePath = filepath.Join(tempReposRoot, strings.ToLower(owner), repoName+".git")
	if err := os.MkdirAll(filepath.Dir(barePath), 0755); err != nil {
		t.Fatalf("mkdir bare parent: %v", err)
	}
	if err := exec.Command("git", "clone", "--bare", workDir, barePath).Run(); err != nil {
		t.Fatalf("clone bare: %v", err)
	}
	if err := db.UpdateRepositoryMetadata(ctx, dbClient, owner, repoName, map[string]interface{}{
		"branches": []string{"main", "feat"},
	}); err != nil {
		t.Fatalf("update branches: %v", err)
	}

	// Open a PR feat -> main.
	prBody, _ := json.Marshal(map[string]string{
		"title": "T", "description": "D", "sourceBranch": "feat", "targetBranch": "main",
	})
	req = httptest.NewRequest("POST", "/api/repos/"+owner+"/"+repoName+"/pulls", bytes.NewBuffer(prBody))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create PR: %d %s", rr.Code, rr.Body.String())
	}
	var pr db.PullRequest
	_ = json.NewDecoder(rr.Body).Decode(&pr)
	return r, owner, repoName, uid, pr.Number, barePath
}

func TestMergePullRequest_BlockedByApprovals(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("db client: %v", err)
	}
	defer client.Close()

	r, owner, repoName, uid, prNum, _ := setupMergeGateFixture(t, ctx, client)

	// Rule on main requiring 1 approval.
	if _, err := db.CreateRule(ctx, client, owner, repoName, gitpkg.Rule{
		Pattern: "main", RequireApprovals: 1,
	}); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	// Attempt merge — should be 403.
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/merge", owner, repoName, prNum), nil)
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (need approvals), got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "requires 1 approvals") {
		t.Errorf("expected approvals-required message, got %q", rr.Body.String())
	}
}

func TestMergePullRequest_BlockedByCodeownerApproval(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("db client: %v", err)
	}
	defer client.Close()

	r, owner, repoName, uid, prNum, _ := setupMergeGateFixture(t, ctx, client)

	// Seed a non-author approval so the count-only gate would pass — but
	// require-codeowner is true and the approver is NOT a codeowner.
	approverUID := "approver-" + randSuffix()
	if err := db.UpsertReview(ctx, client, owner, repoName, prNum, db.Review{
		UID: approverUID, Username: "approver", State: "approved", Body: "lgtm",
	}); err != nil {
		t.Fatalf("UpsertReview: %v", err)
	}

	if _, err := db.CreateRule(ctx, client, owner, repoName, gitpkg.Rule{
		Pattern: "main", RequireApprovals: 1, RequireCodeownerApproval: true,
	}); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	// PR has no requestedReviewers (no CODEOWNERS file in repo), so the
	// codeowner gate must reject regardless of the approval count.
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/merge", owner, repoName, prNum), nil)
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (codeowner), got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "codeowner") {
		t.Errorf("expected codeowner message, got %q", rr.Body.String())
	}
}

func TestMergePullRequest_AllowedWithApprovalAndCodeowner(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("db client: %v", err)
	}
	defer client.Close()

	r, owner, repoName, uid, prNum, _ := setupMergeGateFixture(t, ctx, client)

	// Pretend that "reviewer" is a CODEOWNER for this PR by writing it
	// directly to requestedReviewers. (D5 normally does this from the diff;
	// this fixture skips checking the diff and seeds the field directly.)
	if err := db.UpdatePullRequestReviewers(ctx, client, owner, repoName, prNum, []string{"reviewer"}); err != nil {
		t.Fatalf("UpdatePullRequestReviewers: %v", err)
	}
	// "reviewer" approves.
	if err := db.UpsertReview(ctx, client, owner, repoName, prNum, db.Review{
		UID: "rev-uid-" + randSuffix(), Username: "reviewer", State: "approved", Body: "lgtm",
	}); err != nil {
		t.Fatalf("UpsertReview: %v", err)
	}

	if _, err := db.CreateRule(ctx, client, owner, repoName, gitpkg.Rule{
		Pattern: "main", RequireApprovals: 1, RequireCodeownerApproval: true,
	}); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/merge", owner, repoName, prNum), nil)
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (merge allowed), got %d %s", rr.Code, rr.Body.String())
	}
}

func TestMergePullRequest_BlockedByMergeAllowlist(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("db client: %v", err)
	}
	defer client.Close()

	r, owner, repoName, uid, prNum, _ := setupMergeGateFixture(t, ctx, client)

	// Allowlist contains some other user's uid — owner is not on it.
	if _, err := db.CreateRule(ctx, client, owner, repoName, gitpkg.Rule{
		Pattern: "main", MergeAllowlist: []string{"someone-else-uid"},
	}); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/repos/%s/%s/pulls/%d/merge", owner, repoName, prNum), nil)
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (merge allowlist), got %d %s", rr.Code, rr.Body.String())
	}
}

