package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps"
	"gitbucket/internal/auth"
	"gitbucket/internal/db"
	"gitbucket/internal/gcs"
	gitpkg "gitbucket/internal/git"
)

// branchExists checks if a branch exists in the bare repository.
func branchExists(localRepoPath, branch string) bool {
	cmd := exec.Command("git", "--git-dir", localRepoPath, "show-ref", "--verify", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// getBranchSHA resolves a branch name to its latest commit SHA.
func getBranchSHA(localRepoPath, branch string) (string, error) {
	cmd := exec.Command("git", "--git-dir", localRepoPath, "rev-parse", "refs/heads/"+branch)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runGit runs a git command in the specified directory.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git %v failed: %w, stderr: %s", args, err, stderr.String())
	}
	return stdout.String(), nil
}

// containsString reports whether s is present in xs.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// loadCodeOwnersFromBareRepo reads a CODEOWNERS file from a bare repository at
// the given ref by trying root, .gitbucket/, then docs/ in order. Returns an
// empty (non-nil) *CodeOwners if no file exists at any of those paths.
func loadCodeOwnersFromBareRepo(gitDir, ref string) (*gitpkg.CodeOwners, error) {
	for _, rel := range []string{"CODEOWNERS", ".gitbucket/CODEOWNERS", "docs/CODEOWNERS"} {
		cmd := exec.Command("git", "--git-dir", gitDir, "show", ref+":"+rel)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// "does not exist" / "exists on disk, but not in" — try next location
			continue
		}
		return gitpkg.ParseCodeOwners(&stdout)
	}
	return &gitpkg.CodeOwners{}, nil
}

// resolveCodeOwnersForPR computes the deduplicated set of usernames that should
// be requested as reviewers for the given PR. It diffs target...source for
// changed files, loads CODEOWNERS from the target branch, matches each file,
// and excludes the PR author from the result. The returned slice is sorted.
//
// Best-effort: if anything fails (diff error, missing CODEOWNERS) the function
// returns an empty slice and a nil error so the PR open path doesn't block.
func resolveCodeOwnersForPR(gitDir, sourceBranch, targetBranch, authorUsername string) []string {
	cmd := exec.Command("git", "--git-dir", gitDir, "diff", "--name-only", targetBranch+"..."+sourceBranch)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("codeowners: diff %s...%s failed: %v", targetBranch, sourceBranch, err)
		return nil
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")

	co, err := loadCodeOwnersFromBareRepo(gitDir, targetBranch)
	if err != nil {
		log.Printf("codeowners: load on %s failed: %v", targetBranch, err)
		return nil
	}

	owners := map[string]struct{}{}
	for _, f := range files {
		if f == "" {
			continue
		}
		for _, o := range co.Match(f) {
			u := strings.TrimPrefix(o, "@")
			if u == "" || strings.EqualFold(u, authorUsername) {
				continue
			}
			owners[u] = struct{}{}
		}
	}
	reviewers := make([]string, 0, len(owners))
	for u := range owners {
		reviewers = append(reviewers, u)
	}
	sort.Strings(reviewers)
	return reviewers
}

// CreatePullRequest handles POST /api/repos/{owner}/{repo}/pulls
func (h *APIHandler) CreatePullRequest(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	uid := auth.GetUID(r.Context())
	username := auth.GetUsername(r.Context())
	if uid == "" || username == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Title        string `json:"title"`
		Description  string `json:"description"`
		SourceBranch string `json:"sourceBranch"`
		TargetBranch string `json:"targetBranch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title == "" || req.SourceBranch == "" || req.TargetBranch == "" {
		http.Error(w, "Title, sourceBranch, and targetBranch are required", http.StatusBadRequest)
		return
	}

	// Authorize reading and sync cache
	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	// Verify that source and target branches exist in the repository
	if !branchExists(localRepoPath, req.SourceBranch) {
		http.Error(w, fmt.Sprintf("sourceBranch %q does not exist", req.SourceBranch), http.StatusBadRequest)
		return
	}
	if !branchExists(localRepoPath, req.TargetBranch) {
		http.Error(w, fmt.Sprintf("targetBranch %q does not exist", req.TargetBranch), http.StatusBadRequest)
		return
	}

	pr, err := db.CreatePullRequest(r.Context(), h.FirestoreClient, owner, repo, req.Title, req.Description, req.SourceBranch, req.TargetBranch, uid, username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-resolve CODEOWNERS for the diff and persist as requestedReviewers.
	// Best-effort: failures here are logged and the PR is still returned successfully.
	reviewers := resolveCodeOwnersForPR(localRepoPath, pr.SourceBranch, pr.TargetBranch, pr.AuthorUsername)
	if reviewers == nil {
		reviewers = []string{}
	}
	if err := db.UpdatePullRequestReviewers(r.Context(), h.FirestoreClient, owner, repo, pr.Number, reviewers); err != nil {
		log.Printf("codeowners: set reviewers for PR %d: %v", pr.Number, err)
	}
	pr.RequestedReviewers = reviewers

	apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
		Action:     "opened",
		Number:     pr.Number,
		Title:      pr.Title,
		Body:       pr.Description,
		State:      "open",
		HeadBranch: pr.SourceBranch,
		BaseBranch: pr.TargetBranch,
		OwnerLogin: owner,
		RepoName:   repo,
		Sender:     apps.SenderRef{Login: username, Type: "User"},
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(pr)
}

// ListPullRequests handles GET /api/repos/{owner}/{repo}/pulls
func (h *APIHandler) ListPullRequests(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	_, _, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	prs, err := db.ListPullRequests(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prs)
}

// GetPullRequest handles GET /api/repos/{owner}/{repo}/pulls/{number}
func (h *APIHandler) GetPullRequest(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, number)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pr == nil {
		http.Error(w, "Pull request not found", http.StatusNotFound)
		return
	}

	if pr.Status == "open" {
		// Sync repository first to get the latest refs from GCS
		if h.StorageClient != nil && h.BucketName != "" {
			_, err = gcs.DownloadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
			if err != nil {
				http.Error(w, "Failed to sync repository from GCS: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		sourceSha, errSource := getBranchSHA(localRepoPath, pr.SourceBranch)
		targetSha, errTarget := getBranchSHA(localRepoPath, pr.TargetBranch)
		if errSource == nil && errTarget == nil {
			if pr.Mergeable == nil || pr.LastCheckedSourceSHA != sourceSha || pr.LastCheckedTargetSHA != targetSha {
				// Recheck mergeability
				tempDir, tempErr := os.MkdirTemp("", "gitbucket-mergecheck-")
				if tempErr == nil {
					defer os.RemoveAll(tempDir)
					_, cloneErr := runGit("", "clone", localRepoPath, tempDir)
					if cloneErr == nil {
						_, checkoutErr := runGit(tempDir, "checkout", pr.TargetBranch)
						if checkoutErr == nil {
							_, _ = runGit(tempDir, "config", "user.name", "mergecheck")
							_, _ = runGit(tempDir, "config", "user.email", "mergecheck@example.com")
							_, mergeErr := runGit(tempDir, "merge", "--no-commit", "--no-ff", "origin/"+pr.SourceBranch)
							
							isMergeable := mergeErr == nil
							pr.Mergeable = &isMergeable
							pr.LastCheckedSourceSHA = sourceSha
							pr.LastCheckedTargetSHA = targetSha
							
							_ = db.UpdatePullRequestMergeableStatus(r.Context(), h.FirestoreClient, owner, repo, number, pr.Mergeable, sourceSha, targetSha)
						}
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pr)
}

// GetPullRequestCommits handles GET /api/repos/{owner}/{repo}/pulls/{number}/commits
func (h *APIHandler) GetPullRequestCommits(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, number)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pr == nil {
		http.Error(w, "Pull request not found", http.StatusNotFound)
		return
	}

	cmd := exec.Command("git", "--git-dir", localRepoPath, "log", `--format=%H|%an|%ae|%ad|%s`, fmt.Sprintf("%s..%s", pr.TargetBranch, pr.SourceBranch))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		errStr := stderr.String()
		if strings.Contains(errStr, "does not have any commits") || strings.Contains(errStr, "fatal: bad default revision") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, "Git error: "+errStr, http.StatusInternalServerError)
		return
	}

	var commits []CommitInfo
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, CommitInfo{
			SHA:         parts[0],
			AuthorName:  parts[1],
			AuthorEmail: parts[2],
			Date:        parts[3],
			Message:     parts[4],
		})
	}
	if commits == nil {
		commits = []CommitInfo{}
	} else if len(commits) > 0 {
		h.DecorateCommitsWithStatuses(r.Context(), owner, repo, commits)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(commits)
}

// GetPullRequestDiff handles GET /api/repos/{owner}/{repo}/pulls/{number}/diff
func (h *APIHandler) GetPullRequestDiff(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, number)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pr == nil {
		http.Error(w, "Pull request not found", http.StatusNotFound)
		return
	}

	cmd := exec.Command("git", "--git-dir", localRepoPath, "diff", fmt.Sprintf("%s...%s", pr.TargetBranch, pr.SourceBranch))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		http.Error(w, "Git error: "+stderr.String(), http.StatusInternalServerError)
		return
	}

	resp := map[string]string{
		"rawDiff": stdout.String(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ClosePullRequest handles POST /api/repos/{owner}/{repo}/pulls/{number}/close
func (h *APIHandler) ClosePullRequest(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	uid := auth.GetUID(r.Context())
	username := auth.GetUsername(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	_, _, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, number)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pr == nil {
		http.Error(w, "Pull request not found", http.StatusNotFound)
		return
	}

	if pr.Status != "open" {
		http.Error(w, "Pull request is not open", http.StatusBadRequest)
		return
	}

	isOwner := strings.EqualFold(username, owner)
	isAuthor := pr.AuthorUID == uid
	if !isOwner && !isAuthor {
		http.Error(w, "Forbidden: not authorized to close this pull request", http.StatusForbidden)
		return
	}

	err = db.UpdatePullRequestStatus(r.Context(), h.FirestoreClient, owner, repo, number, "closed")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
		Action:     "closed",
		Number:     pr.Number,
		Title:      pr.Title,
		Body:       pr.Description,
		State:      "closed",
		HeadBranch: pr.SourceBranch,
		BaseBranch: pr.TargetBranch,
		OwnerLogin: owner,
		RepoName:   repo,
		Sender:     apps.SenderRef{Login: username, Type: "User"},
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "closed",
	})
}

// MergePullRequest handles POST /api/repos/{owner}/{repo}/pulls/{number}/merge
func (h *APIHandler) MergePullRequest(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	uid := auth.GetUID(r.Context())
	username := auth.GetUsername(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Only repo owner is authorized to merge
	isOwner := strings.EqualFold(username, owner)
	if !isOwner {
		http.Error(w, "Forbidden: only the repository owner can merge pull requests", http.StatusForbidden)
		return
	}

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, number)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pr == nil {
		http.Error(w, "Pull request not found", http.StatusNotFound)
		return
	}

	if pr.Status != "open" {
		http.Error(w, "Pull request is not open", http.StatusBadRequest)
		return
	}

	// Branch-protection + CODEOWNERS enforcement on the target branch.
	rules, ruleErr := db.ListRules(r.Context(), h.FirestoreClient, owner, repo)
	if ruleErr != nil {
		http.Error(w, "Failed to load branch protection rules: "+ruleErr.Error(), http.StatusInternalServerError)
		return
	}
	if rule := gitpkg.MatchRule(rules, pr.TargetBranch); rule != nil {
		// Merge allowlist: when non-empty, only listed UIDs may merge. Repo
		// owner is allowed implicitly (they passed the isOwner check above).
		if len(rule.MergeAllowlist) > 0 && !containsString(rule.MergeAllowlist, uid) {
			http.Error(w, "merge not allowed for this user by branch protection rule", http.StatusForbidden)
			return
		}

		if rule.RequireApprovals > 0 || rule.RequireCodeownerApproval {
			reviews, err := db.ListReviews(r.Context(), h.FirestoreClient, owner, repo, pr.Number)
			if err != nil {
				http.Error(w, "Failed to load reviews: "+err.Error(), http.StatusInternalServerError)
				return
			}
			approvals := 0
			approvalUsernames := map[string]bool{}
			for _, rv := range reviews {
				if rv.State == "approved" {
					approvals++
					approvalUsernames[rv.Username] = true
				}
			}
			if approvals < rule.RequireApprovals {
				http.Error(w, fmt.Sprintf("merge requires %d approvals (have %d)", rule.RequireApprovals, approvals), http.StatusForbidden)
				return
			}
			if rule.RequireCodeownerApproval {
				ok := false
				for _, ru := range pr.RequestedReviewers {
					if approvalUsernames[ru] {
						ok = true
						break
					}
				}
				if !ok {
					http.Error(w, "merge requires approval from a codeowner", http.StatusForbidden)
					return
				}
			}
		}
	}

	// 1. Acquire Lock
	lockToken, err := db.AcquireLock(r.Context(), h.FirestoreClient, owner, repo, 5*time.Minute, 30*time.Second)
	if err != nil {
		http.Error(w, "Failed to acquire repository lock: "+err.Error(), http.StatusConflict)
		return
	}
	defer func() {
		if lockToken != "" {
			_ = db.ReleaseLock(context.Background(), h.FirestoreClient, owner, repo, lockToken)
		}
	}()

	// Download/sync repository again under the lock to ensure we have the absolute latest state
	if h.StorageClient != nil && h.BucketName != "" {
		_, err = gcs.DownloadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
		if err != nil {
			http.Error(w, "Failed to sync repository from GCS: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Verify that source and target branches still exist in the repository
	if !branchExists(localRepoPath, pr.SourceBranch) {
		http.Error(w, fmt.Sprintf("sourceBranch %q does not exist", pr.SourceBranch), http.StatusBadRequest)
		return
	}
	if !branchExists(localRepoPath, pr.TargetBranch) {
		http.Error(w, fmt.Sprintf("targetBranch %q does not exist", pr.TargetBranch), http.StatusBadRequest)
		return
	}

	// 2. Clone/merge using a temporary directory
	tempDir, err := os.MkdirTemp("", "gitbucket-merge-")
	if err != nil {
		http.Error(w, "Failed to create temporary directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	// Clone bare repository to temporary directory
	_, err = runGit("", "clone", localRepoPath, tempDir)
	if err != nil {
		http.Error(w, "Clone failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Checkout target branch
	_, err = runGit(tempDir, "checkout", pr.TargetBranch)
	if err != nil {
		http.Error(w, "Checkout target branch failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Configure git committer & author identity locally in the clone
	_, _ = runGit(tempDir, "config", "user.name", username)
	_, _ = runGit(tempDir, "config", "user.email", username+"@example.com")

	// Merge source branch
	mergeMsg := fmt.Sprintf("Merge pull request #%d from %s", pr.Number, pr.SourceBranch)
	_, err = runGit(tempDir, "merge", "--no-ff", "origin/"+pr.SourceBranch, "-m", mergeMsg)
	if err != nil {
		// This usually means there's a merge conflict
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Merge conflict or error: " + err.Error(),
		})
		return
	}

	// Push merged branch back to bare repository
	_, err = runGit(tempDir, "push", "origin", pr.TargetBranch)
	if err != nil {
		http.Error(w, "Failed to push merged changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Auto-delete head branch if configured
	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err == nil && repoMeta != nil {
		autoDelete, _ := repoMeta["autoDeleteHeadBranches"].(bool)
		defaultBranch, _ := repoMeta["defaultBranch"].(string)
		if defaultBranch == "" {
			defaultBranch = "main"
		}
		if autoDelete && pr.SourceBranch != defaultBranch {
			_, deleteErr := runGit(localRepoPath, "branch", "-D", pr.SourceBranch)
			if deleteErr != nil {
				log.Printf("[Merge PR] Failed to delete source branch %s automatically: %v", pr.SourceBranch, deleteErr)
			} else {
				log.Printf("[Merge PR] Automatically deleted head branch %s", pr.SourceBranch)
			}
		}
	}

	// 4. Upload updated bare repo to GCS
	if h.StorageClient != nil && h.BucketName != "" {
		err = gcs.UploadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
		if err != nil {
			http.Error(w, "Failed to upload repository to GCS: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Update repository branch metadata
	branches := getRepoBranches(localRepoPath)
	err = db.UpdateRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo, map[string]interface{}{
		"branches": branches,
	})
	if err != nil {
		log.Printf("Failed to update repository metadata: %v", err)
	}

	updatedMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err == nil && updatedMeta != nil {
		_ = writeLocalSyncTimestamp(localRepoPath, getUpdatedAtMs(updatedMeta))
	}

	// Update PR status in Firestore
	err = db.UpdatePullRequestStatus(r.Context(), h.FirestoreClient, owner, repo, pr.Number, "merged")
	if err != nil {
		http.Error(w, "Failed to update pull request status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
		Action:     "closed",
		Number:     pr.Number,
		Title:      pr.Title,
		Body:       pr.Description,
		State:      "merged",
		HeadBranch: pr.SourceBranch,
		BaseBranch: pr.TargetBranch,
		OwnerLogin: owner,
		RepoName:   repo,
		Sender:     apps.SenderRef{Login: username, Type: "User"},
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "merged",
	})
}

// UpdatePullRequestBranch handles POST /api/repos/{owner}/{repo}/pulls/{number}/update
func (h *APIHandler) UpdatePullRequestBranch(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	uid := auth.GetUID(r.Context())
	username := auth.GetUsername(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, number)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pr == nil {
		http.Error(w, "Pull request not found", http.StatusNotFound)
		return
	}

	if pr.Status != "open" {
		http.Error(w, "Pull request is not open", http.StatusBadRequest)
		return
	}

	// Read update strategy from request body
	var reqBody struct {
		Strategy string `json:"strategy"` // "merge" or "rebase"
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if reqBody.Strategy != "merge" && reqBody.Strategy != "rebase" {
		http.Error(w, "Invalid strategy (must be 'merge' or 'rebase')", http.StatusBadRequest)
		return
	}

	// 1. Acquire Lock
	lockToken, err := db.AcquireLock(r.Context(), h.FirestoreClient, owner, repo, 5*time.Minute, 30*time.Second)
	if err != nil {
		http.Error(w, "Failed to acquire repository lock: "+err.Error(), http.StatusConflict)
		return
	}
	defer func() {
		if lockToken != "" {
			_ = db.ReleaseLock(context.Background(), h.FirestoreClient, owner, repo, lockToken)
		}
	}()

	// Sync from GCS under the lock
	if h.StorageClient != nil && h.BucketName != "" {
		_, err = gcs.DownloadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
		if err != nil {
			http.Error(w, "Failed to sync repository from GCS: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Verify branches exist
	if !branchExists(localRepoPath, pr.SourceBranch) {
		http.Error(w, fmt.Sprintf("sourceBranch %q does not exist", pr.SourceBranch), http.StatusBadRequest)
		return
	}
	if !branchExists(localRepoPath, pr.TargetBranch) {
		http.Error(w, fmt.Sprintf("targetBranch %q does not exist", pr.TargetBranch), http.StatusBadRequest)
		return
	}

	// 2. Clone using a temporary directory
	tempDir, err := os.MkdirTemp("", "gitbucket-update-")
	if err != nil {
		http.Error(w, "Failed to create temporary directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	// Clone bare repo to temp
	_, err = runGit("", "clone", localRepoPath, tempDir)
	if err != nil {
		http.Error(w, "Clone failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Checkout source branch (the branch we want to update)
	_, err = runGit(tempDir, "checkout", pr.SourceBranch)
	if err != nil {
		http.Error(w, "Checkout source branch failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Configure git committer & author identity locally in the clone
	_, _ = runGit(tempDir, "config", "user.name", username)
	_, _ = runGit(tempDir, "config", "user.email", username+"@example.com")

	if reqBody.Strategy == "merge" {
		mergeMsg := fmt.Sprintf("Merge branch '%s' into '%s'", pr.TargetBranch, pr.SourceBranch)
		_, err = runGit(tempDir, "merge", "--no-ff", "origin/"+pr.TargetBranch, "-m", mergeMsg)
		if err != nil {
			// Merge conflict!
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("Merge conflict: Could not merge '%s' into '%s' cleanly. Details: %v", pr.TargetBranch, pr.SourceBranch, err.Error()),
			})
			return
		}
	} else { // rebase
		_, err = runGit(tempDir, "rebase", "origin/"+pr.TargetBranch)
		if err != nil {
			// Rebase conflict! Clean up the rebase state first
			_, _ = runGit(tempDir, "rebase", "--abort")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("Rebase conflict: Could not rebase '%s' onto '%s' cleanly. Details: %v", pr.SourceBranch, pr.TargetBranch, err.Error()),
			})
			return
		}
	}

	// Push updated source branch back to origin
	// (We use --force if it is a rebase since rebase rewrites git history)
	pushArgs := []string{"push", "origin", pr.SourceBranch}
	if reqBody.Strategy == "rebase" {
		pushArgs = append(pushArgs, "--force")
	}
	_, err = runGit(tempDir, pushArgs...)
	if err != nil {
		http.Error(w, "Failed to push updated branch: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Upload updated bare repo to GCS
	if h.StorageClient != nil && h.BucketName != "" {
		err = gcs.UploadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
		if err != nil {
			http.Error(w, "Failed to upload updated repository to GCS: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// 4. Update mergeability status cache on the PR in Firestore
	sourceSha, errSource := getBranchSHA(localRepoPath, pr.SourceBranch)
	targetSha, errTarget := getBranchSHA(localRepoPath, pr.TargetBranch)
	if errSource == nil && errTarget == nil {
		isMergeable := true
		pr.Mergeable = &isMergeable
		pr.LastCheckedSourceSHA = sourceSha
		pr.LastCheckedTargetSHA = targetSha
		_ = db.UpdatePullRequestMergeableStatus(r.Context(), h.FirestoreClient, owner, repo, number, pr.Mergeable, sourceSha, targetSha)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "updated",
	})
}

