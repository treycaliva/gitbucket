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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/errgroup"

	"gitbucket/internal/apps"
	"gitbucket/internal/auth"
	"gitbucket/internal/db"
	"gitbucket/internal/gcs"
	"gitbucket/internal/repocache"
	"gitbucket/internal/sync"
)

var usernameRegex = regexp.MustCompile("^[a-zA-Z0-9-]{3,20}$")
var repoNameRegex = regexp.MustCompile("^[a-zA-Z0-9-_]{3,30}$")

// APIHandler handles user and PAT REST endpoints.
type APIHandler struct {
	FirestoreClient *firestore.Client
	StorageClient   *storage.Client
	BucketName      string
	LocalReposRoot  string
	AuthHandler     *auth.AuthHandler
	KMSClient       *sync.KMSClient
	Events          apps.FireDeps      // populated by main.go after construction
	RepoCache       *repocache.Manager // bounds LOCAL_REPOS_ROOT disk use; nil = unbounded
}

// evictRepos runs an LRU eviction sweep over the local repo cache, using the
// per-repo Firestore lock to avoid deleting a repo mid-push on any instance.
// Safe to call when RepoCache is nil or disabled. Uses a background context so
// it isn't cancelled when the triggering request finishes.
func (h *APIHandler) evictRepos() {
	if !h.RepoCache.Enabled() {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[repocache] eviction sweep panicked: %v", rec)
		}
	}()
	h.RepoCache.Sweep(context.Background(), func(owner, repo string) (func(), bool) {
		token, err := db.AcquireLock(context.Background(), h.FirestoreClient, owner, repo, 5*time.Minute, 0)
		if err != nil {
			return nil, false
		}
		return func() {
			_ = db.ReleaseLock(context.Background(), h.FirestoreClient, owner, repo, token)
		}, true
	})
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(client *firestore.Client, storageClient *storage.Client, bucketName, localReposRoot string, authHandler *auth.AuthHandler, kmsClient *sync.KMSClient) *APIHandler {
	return &APIHandler{
		FirestoreClient: client,
		StorageClient:   storageClient,
		BucketName:      bucketName,
		LocalReposRoot:  localReposRoot,
		AuthHandler:     authHandler,
		KMSClient:       kmsClient,
	}
}

// RegisterRoutes registers the /api endpoints on the router.
func (h *APIHandler) RegisterRoutes(r chi.Router) {
	r.Post("/r/{owner}/{repo}/info/lfs/objects/batch", h.HandleLFSBatch)
	r.Post("/r/{owner}/{repo}.git/info/lfs/objects/batch", h.HandleLFSBatch)
	r.Put("/r/{owner}/{repo}/info/lfs/objects/{oid}", h.HandleLFSUpload)
	r.Put("/r/{owner}/{repo}.git/info/lfs/objects/{oid}", h.HandleLFSUpload)
	r.Get("/r/{owner}/{repo}/info/lfs/objects/{oid}", h.HandleLFSDownload)
	r.Get("/r/{owner}/{repo}.git/info/lfs/objects/{oid}", h.HandleLFSDownload)

	r.HandleFunc("/r/{owner}/{repo}", h.HandleGitHTTP)
	r.HandleFunc("/r/{owner}/{repo}/*", h.HandleGitHTTP)

	r.Route("/api", func(r chi.Router) {
		// Public health check is mounted directly or here:
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		})

		r.Get("/config", h.GetConfig)
		r.Post("/webhooks/github", h.HandleGitHubWebhook)
		r.Post("/webhooks/cloudbuild", h.HandleCloudBuildWebhook)
		r.Post("/tasks/sync-github", h.HandleSyncTask)

		// Optional auth group
		r.Group(func(r chi.Router) {
			r.Use(h.AuthHandler.OptionalWebAuth)
			r.Get("/repos", h.ListRepositories)
			r.Get("/repos/{owner}/{repo}", h.GetRepository)
			r.Get("/repos/{owner}/{repo}/commits/{branch}", h.GetCommitHistory)
			r.Get("/repos/{owner}/{repo}/commit/{sha}", h.GetCommitDiff)
			r.Get("/repos/{owner}/{repo}/tree/{branch}", h.GetTree)
			r.Get("/repos/{owner}/{repo}/tree/{branch}/*", h.GetTree)
			r.Get("/repos/{owner}/{repo}/blob/{branch}/*", h.GetBlob)
			r.Get("/repos/{owner}/{repo}/codeowners", h.CodeOwnersHandler)
			r.Get("/repos/{owner}/{repo}/tags", h.ListTags)
			r.Get("/repos/{owner}/{repo}/refs/{branch}/head", h.GetBranchHead)

			r.Get("/repos/{owner}/{repo}/pulls", h.ListPullRequests)
			r.Get("/repos/{owner}/{repo}/pulls/{number}", h.GetPullRequest)
			r.Get("/repos/{owner}/{repo}/pulls/{number}/commits", h.GetPullRequestCommits)
			r.Get("/repos/{owner}/{repo}/pulls/{number}/diff", h.GetPullRequestDiff)
			r.Get("/repos/{owner}/{repo}/pulls/{number}/reviews", h.ListReviewsHandler)

			r.Get("/repos/{owner}/{repo}/collaborators", h.ListCollaboratorsHandler)

			r.Get("/repos/{owner}/{repo}/branch-protection", h.ListBranchProtectionHandler)

			r.Get("/repos/{owner}/{repo}/sync/wait", h.WaitSync)
			r.Get("/repos/{owner}/{repo}/builds/{buildId}/logs", h.GetBuildLogsSignedURL)
			r.Get("/repos/{owner}/{repo}/builds/{buildId}/logs/stream", h.HandleLiveLogsStream)
		})

		// Authenticated group
		r.Group(func(r chi.Router) {
			r.Use(h.AuthHandler.RequireWebAuth)

			r.Post("/user/username", h.RegisterUsername)
			r.Get("/user/me", h.GetProfile)

			r.Get("/tokens", h.ListTokens)
			r.Post("/tokens", h.GenerateToken)
			r.Delete("/tokens/{tokenId}", h.RevokeToken)

			r.Post("/repos", h.CreateRepository)
			r.Delete("/repos/{owner}/{repo}", h.DeleteRepository)
			r.Patch("/repos/{owner}/{repo}", h.UpdateRepository)
			r.Delete("/repos/{owner}/{repo}/branches/{branch}", h.DeleteBranch)

			r.Post("/repos/{owner}/{repo}/pulls", h.CreatePullRequest)
			r.Post("/repos/{owner}/{repo}/pulls/{number}/merge", h.MergePullRequest)
			r.Post("/repos/{owner}/{repo}/pulls/{number}/close", h.ClosePullRequest)
			r.Post("/repos/{owner}/{repo}/pulls/{number}/update", h.UpdatePullRequestBranch)
			r.Post("/repos/{owner}/{repo}/pulls/{number}/reviews", h.SubmitReviewHandler)

			r.Post("/repos/{owner}/{repo}/collaborators", h.AddCollaboratorHandler)
			r.Delete("/repos/{owner}/{repo}/collaborators/{username}", h.RemoveCollaboratorHandler)

			r.Post("/repos/{owner}/{repo}/branch-protection", h.CreateBranchProtectionHandler)
			r.Put("/repos/{owner}/{repo}/branch-protection/{ruleId}", h.UpdateBranchProtectionHandler)
			r.Delete("/repos/{owner}/{repo}/branch-protection/{ruleId}", h.DeleteBranchProtectionHandler)

			r.Get("/repos/{owner}/{repo}/sync/config", h.GetSyncConfig)
			r.Post("/repos/{owner}/{repo}/sync/config", h.UpdateSyncConfig)
			r.Post("/repos/{owner}/{repo}/sync/resolve", h.ResolveSyncConflict)
			r.Post("/repos/{owner}/{repo}/builds/ticket", h.GetLogTicket)
		})
	})
}

// RegisterUsername handles POST /api/user/username
func (h *APIHandler) RegisterUsername(w http.ResponseWriter, r *http.Request) {
	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized: missing UID", http.StatusUnauthorized)
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if apps.IsBotUsername(req.Username) {
		http.Error(w, "username is reserved", http.StatusBadRequest)
		return
	}

	if !usernameRegex.MatchString(req.Username) {
		http.Error(w, "Invalid username format", http.StatusBadRequest)
		return
	}

	// Fetch email from FirebaseAuth (or use fallback)
	var email string
	if h.AuthHandler != nil && h.AuthHandler.FirebaseAuth != nil && !h.AuthHandler.DevMode {
		u, err := h.AuthHandler.FirebaseAuth.GetUser(r.Context(), uid)
		if err == nil {
			email = u.Email
		}
	}
	if email == "" {
		email = uid + "@example.com"
	}

	err := db.RegisterUsername(r.Context(), h.FirestoreClient, uid, req.Username, email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"username": req.Username,
	})
}

// GetProfile handles GET /api/user/me
func (h *APIHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var username string
	var email string

	doc, err := h.FirestoreClient.Collection("users").Doc(uid).Get(r.Context())
	if err == nil && doc.Exists() {
		var userData map[string]interface{}
		if err := doc.DataTo(&userData); err == nil {
			username, _ = userData["username"].(string)
			email, _ = userData["email"].(string)
		}
	}

	if username == "" {
		http.Error(w, "Profile not found: please register a username", http.StatusNotFound)
		return
	}

	if email == "" {
		if h.AuthHandler != nil && h.AuthHandler.FirebaseAuth != nil {
			u, err := h.AuthHandler.FirebaseAuth.GetUser(r.Context(), uid)
			if err == nil {
				email = u.Email
			}
		}
		if email == "" {
			email = uid + "@example.com"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"uid":      uid,
		"email":    email,
		"username": username,
	})
}

// ListTokens handles GET /api/tokens
func (h *APIHandler) ListTokens(w http.ResponseWriter, r *http.Request) {
	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	tokens, err := db.ListPATs(r.Context(), h.FirestoreClient, uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if tokens == nil {
		tokens = []db.PATMetadata{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tokens)
}

// GenerateToken handles POST /api/tokens
func (h *APIHandler) GenerateToken(w http.ResponseWriter, r *http.Request) {
	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Token name is required", http.StatusBadRequest)
		return
	}

	token, err := db.GeneratePAT(r.Context(), h.FirestoreClient, uid, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": token,
		"name":  req.Name,
	})
}

// RevokeToken handles DELETE /api/tokens/{tokenId}
func (h *APIHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	tokenId := chi.URLParam(r, "tokenId")
	if tokenId == "" {
		http.Error(w, "Token ID is required", http.StatusBadRequest)
		return
	}

	err := db.RevokePAT(r.Context(), h.FirestoreClient, uid, tokenId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"success": true,
	})
}

// ListRepositories handles GET /api/repos
func (h *APIHandler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	publicRepos, err := db.ListAllPublicRepositories(ctx, h.FirestoreClient)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	username := auth.GetUsername(ctx)
	if username == "" {
		w.Header().Set("Content-Type", "application/json")
		if publicRepos == nil {
			publicRepos = []map[string]interface{}{}
		}
		_ = json.NewEncoder(w).Encode(publicRepos)
		return
	}

	userRepos, err := db.ListUserRepositories(ctx, h.FirestoreClient, username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// De-duplicate by owner/name (lowercased)
	allReposMap := make(map[string]map[string]interface{})
	for _, repo := range publicRepos {
		owner, _ := repo["owner"].(string)
		name, _ := repo["name"].(string)
		key := strings.ToLower(owner) + "/" + strings.ToLower(name)
		allReposMap[key] = repo
	}
	for _, repo := range userRepos {
		owner, _ := repo["owner"].(string)
		name, _ := repo["name"].(string)
		key := strings.ToLower(owner) + "/" + strings.ToLower(name)
		allReposMap[key] = repo
	}

	var list []map[string]interface{}
	for _, repo := range allReposMap {
		list = append(list, repo)
	}
	if list == nil {
		list = []map[string]interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// CreateRepository handles POST /api/repos
func (h *APIHandler) CreateRepository(w http.ResponseWriter, r *http.Request) {
	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	username := auth.GetUsername(r.Context())
	if username == "" {
		http.Error(w, "Forbidden: you must set a username before creating a repository", http.StatusForbidden)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if !repoNameRegex.MatchString(req.Name) {
		http.Error(w, "Repository name must be 3-30 characters, alphanumeric, hyphens, or underscores", http.StatusBadRequest)
		return
	}

	visibility := req.Visibility
	if visibility != "private" {
		visibility = "public"
	}

	err := db.CreateRepositoryMetadata(r.Context(), h.FirestoreClient, uid, username, req.Name, req.Description, visibility)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"owner":   username,
		"name":    req.Name,
	})
}

// GetRepository handles GET /api/repos/{owner}/{repo}
func (h *APIHandler) GetRepository(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	meta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if meta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	visibility, _ := meta["visibility"].(string)
	requesterUid := auth.GetUID(r.Context())
	ownerUid, _ := meta["ownerUid"].(string)

	if visibility == "private" {
		if requesterUid == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if requesterUid != ownerUid {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

// DeleteRepository handles DELETE /api/repos/{owner}/{repo}
func (h *APIHandler) DeleteRepository(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	meta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if meta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	ownerUid, _ := meta["ownerUid"].(string)
	if ownerUid != uid {
		http.Error(w, "Forbidden: only the repository owner can delete this repository", http.StatusForbidden)
		return
	}

	// 1. Delete from GCS
	if h.StorageClient != nil && h.BucketName != "" {
		err = gcs.DeleteRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete repository from GCS: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// 2. Delete Firestore metadata
	err = db.DeleteRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete repository metadata: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// UpdateRepository handles PATCH /api/repos/{owner}/{repo}
func (h *APIHandler) UpdateRepository(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	meta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("[UpdateRepository] GetRepositoryMetadata(%s/%s) failed: %v", owner, repo, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if meta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	ownerUid, _ := meta["ownerUid"].(string)
	if ownerUid != uid {
		http.Error(w, "Forbidden: only the repository owner can modify settings", http.StatusForbidden)
		return
	}

	var req struct {
		Description            *string `json:"description"`
		Visibility             *string `json:"visibility"`
		AutoDeleteHeadBranches *bool   `json:"autoDeleteHeadBranches"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	updates := make(map[string]interface{})
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Visibility != nil {
		v := strings.ToLower(*req.Visibility)
		if v != "public" && v != "private" {
			http.Error(w, "Visibility must be public or private", http.StatusBadRequest)
			return
		}
		updates["visibility"] = v
	}
	if req.AutoDeleteHeadBranches != nil {
		updates["autoDeleteHeadBranches"] = *req.AutoDeleteHeadBranches
	}

	if len(updates) > 0 {
		updateKeys := make([]string, 0, len(updates))
		for k := range updates {
			updateKeys = append(updateKeys, k)
		}
		log.Printf("[UpdateRepository] updating %s/%s with keys=%v", owner, repo, updateKeys)
		err = db.UpdateRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo, updates)
		if err != nil {
			log.Printf("[UpdateRepository] UpdateRepositoryMetadata(%s/%s) failed: %v", owner, repo, err)
			http.Error(w, fmt.Sprintf("Failed to update repository settings: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Fetch updated metadata and return it
	updatedMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("[UpdateRepository] GetRepositoryMetadata(%s/%s) after-update failed: %v", owner, repo, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(db.MapToRepositoryMetadata(updatedMeta))
}

// DeleteBranch handles DELETE /api/repos/{owner}/{repo}/branches/{branch}
func (h *APIHandler) DeleteBranch(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	branch := chi.URLParam(r, "branch")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	metaMap, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if metaMap == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}
	meta := db.MapToRepositoryMetadata(metaMap)

	if meta.OwnerUID != uid {
		http.Error(w, "Forbidden: only the repository owner can delete branches", http.StatusForbidden)
		return
	}

	defaultBranch := meta.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	if branch == defaultBranch {
		http.Error(w, "Cannot delete the default branch", http.StatusBadRequest)
		return
	}

	// 1. Acquire repo lock
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

	ownerLower := strings.ToLower(owner)
	repoLower := strings.ToLower(repo)
	localRepoPath := filepath.Join(h.LocalReposRoot, ownerLower, fmt.Sprintf("%s.git", repoLower))

	// Sync local cache from GCS. Lock is held (acquired above), so prune to a
	// clean mirror before mutating branches.
	if h.StorageClient != nil && h.BucketName != "" {
		_, err = gcs.DownloadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot, true)
		if err != nil {
			http.Error(w, "Failed to sync repository from GCS: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// If the branch exists locally, delete it and sync back to GCS.
	// If it doesn't, fall through — the metadata entry is stale and will be
	// reconciled below when we refresh `branches` from the real repo.
	if branchExists(localRepoPath, branch) {
		_, err = runGit(localRepoPath, "branch", "-D", branch)
		if err != nil {
			http.Error(w, "Failed to delete branch: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if h.StorageClient != nil && h.BucketName != "" {
			err = gcs.UploadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
			if err != nil {
				http.Error(w, "Failed to upload repository to GCS: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	// 4. Update repository metadata
	branches := getRepoBranches(localRepoPath)
	err = db.UpdateRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo, map[string]interface{}{
		"branches": branches,
	})
	if err != nil {
		log.Printf("Failed to update repository metadata: %v", err)
	}

	// Update local sync timestamp
	updatedMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err == nil && updatedMeta != nil {
		_ = writeLocalSyncTimestamp(localRepoPath, getUpdatedAtMs(updatedMeta))

		// Trigger GitHub Sync if configured
		if ghSyncVal, ok := updatedMeta["githubSync"]; ok {
			if ghSync, ok := ghSyncVal.(map[string]interface{}); ok {
				enabled, _ := ghSync["enabled"].(bool)
				direction, _ := ghSync["direction"].(string)
				if enabled && (direction == "mirror-to-github" || direction == "bidirectional") {
					h.TriggerSyncTask(owner, repo, direction, false, "")
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"branch":  branch,
	})
}

func ensureBareRepo(localRepoPath string) error {
	if _, err := os.Stat(localRepoPath); err == nil {
		return nil
	}

	if err := os.MkdirAll(localRepoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repository directory: %w", err)
	}

	cmd := exec.Command("git", "init", "--bare", localRepoPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to git init bare: %w", err)
	}

	cmd = exec.Command("git", "-C", localRepoPath, "config", "http.receivepack", "true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure receivepack: %w", err)
	}

	cmd = exec.Command("git", "-C", localRepoPath, "config", "http.uploadpack", "true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure uploadpack: %w", err)
	}

	cmd = exec.Command("git", "-C", localRepoPath, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set default branch: %w", err)
	}

	return nil
}

// authorizeGitRead fetches repository metadata, checks visibility/permissions, and syncs GCS cache.
func (h *APIHandler) authorizeGitRead(w *http.ResponseWriter, r *http.Request, owner, repo string) (*db.RepositoryMetadata, string, bool) {
	ctx := r.Context()
	metaMap, err := db.GetRepositoryMetadata(ctx, h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("[authorizeGitRead] Error getting repository metadata for %s/%s: %v", owner, repo, err)
		http.Error(*w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return nil, "", false
	}
	if metaMap == nil {
		log.Printf("[authorizeGitRead] Repository metadata not found for %s/%s", owner, repo)
		http.Error(*w, "Repository not found", http.StatusNotFound)
		return nil, "", false
	}

	meta := db.MapToRepositoryMetadata(metaMap)

	if meta.Visibility == "private" {
		uid := auth.GetUID(ctx)
		if uid == "" {
			http.Error(*w, "Unauthorized: private repository", http.StatusUnauthorized)
			return nil, "", false
		}
		username := auth.GetUsername(ctx)
		if username == "" || !strings.EqualFold(username, owner) {
			http.Error(*w, "Forbidden: private repository", http.StatusForbidden)
			return nil, "", false
		}
	}

	// Sync local cache. Read-only path with no repo lock held, so merge only —
	// pruning here could race a concurrent push writing local refs.
	if h.StorageClient != nil && h.BucketName != "" {
		_, err = gcs.DownloadRepo(ctx, h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot, false)
		if err != nil {
			log.Printf("[authorizeGitRead] Error downloading repo %s/%s from GCS: %v", owner, repo, err)
			http.Error(*w, "Failed to download repository: "+err.Error(), http.StatusInternalServerError)
			return nil, "", false
		}
	}

	localRepoPath := filepath.Join(h.LocalReposRoot, strings.ToLower(owner), fmt.Sprintf("%s.git", strings.ToLower(repo)))

	if err := ensureBareRepo(localRepoPath); err != nil {
		log.Printf("[authorizeGitRead] Error ensuring bare repo for %s/%s: %v", owner, repo, err)
		http.Error(*w, "Failed to initialize repository: "+err.Error(), http.StatusInternalServerError)
		return nil, "", false
	}

	// Record access so a recently-browsed repo isn't the first thing evicted.
	// Browse git subprocesses are short-lived, so the grace period (not a pin)
	// is enough to keep them safe from an in-flight sweep.
	h.RepoCache.Touch(owner, repo)

	return meta, localRepoPath, true
}

// CommitStatus represents a build status.
type CommitStatus struct {
	BuildID string `json:"buildId"`
	Status  string `json:"status"`
}

// CommitInfo represents basic commit details.
type CommitInfo struct {
	SHA           string         `json:"sha"`
	AuthorName    string         `json:"authorName"`
	AuthorEmail   string         `json:"authorEmail"`
	Date          string         `json:"date"`
	Message       string         `json:"message"`
	Status        string         `json:"status,omitempty"`
	BuildID       string         `json:"buildId,omitempty"`
	OverallStatus string         `json:"overallStatus,omitempty"`
	Statuses      []CommitStatus `json:"statuses,omitempty"`
}

// DecorateCommitsWithStatuses fetches build statuses from Firestore and attaches them to commits.
func (h *APIHandler) DecorateCommitsWithStatuses(ctx context.Context, owner, repo string, commits []CommitInfo) {
	if len(commits) == 0 {
		return
	}

	uniqueShasMap := make(map[string]struct{})
	for _, c := range commits {
		if c.SHA != "" {
			uniqueShasMap[c.SHA] = struct{}{}
		}
	}
	var shas []string
	for sha := range uniqueShasMap {
		shas = append(shas, sha)
	}
	if len(shas) == 0 {
		return
	}

	shaToStatus := make(map[string]map[string]interface{})
	var shaToStatusMutex gosync.Mutex

	// Optimization: Parallelize chunked Firestore queries
	// Firestore limits 'in' queries to 30 items. Previously, chunks were queried sequentially.
	// By running these concurrently using errgroup with a bounded limit, we significantly reduce
	// network latency (expected ~50-80% speedup depending on commit history size).
	// We use `gosync` (aliased stdlib sync) as per memory guidelines.
	chunkSize := 30
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(10) // Limit concurrency to avoid socket exhaustion

	for i := 0; i < len(shas); i += chunkSize {
		end := i + chunkSize
		if end > len(shas) {
			end = len(shas)
		}
		chunk := shas[i:end]

		g.Go(func() error {
			statusQuery := h.FirestoreClient.Collection("commit_statuses").
				Where("owner", "==", owner).
				Where("repo", "==", repo).
				Where("sha", "in", chunk)

			statusDocs, err := statusQuery.Documents(gCtx).GetAll()
			if err != nil {
				log.Printf("DecorateCommitsWithStatuses: Firestore query failed for chunk: %v", err)
				return nil // Continue despite error to mimic previous behavior
			}

			// Pre-parse the documents before acquiring the lock to minimize contention
			type parsedStatus struct {
				sha  string
				data map[string]interface{}
			}
			var parsedStatuses []parsedStatus
			for _, doc := range statusDocs {
				var data map[string]interface{}
				if err := doc.DataTo(&data); err == nil {
					sha, _ := data["sha"].(string)
					if sha != "" {
						parsedStatuses = append(parsedStatuses, parsedStatus{sha, data})
					}
				}
			}

			shaToStatusMutex.Lock()
			defer shaToStatusMutex.Unlock()

			for _, ps := range parsedStatuses {
				existing, exists := shaToStatus[ps.sha]
				if !exists {
					shaToStatus[ps.sha] = ps.data
				} else {
					var curCreated, newCreated time.Time
					if t, ok := existing["createdAt"].(time.Time); ok {
						curCreated = t
					}
					if t, ok := ps.data["createdAt"].(time.Time); ok {
						newCreated = t
					}
					if newCreated.After(curCreated) {
						shaToStatus[ps.sha] = ps.data
					}
				}
			}
			return nil
		})
	}

	_ = g.Wait()

	for i := range commits {
		if statusData, ok := shaToStatus[commits[i].SHA]; ok {
			statusVal, _ := statusData["status"].(string)
			buildIDVal, _ := statusData["buildId"].(string)
			commits[i].Status = statusVal
			commits[i].BuildID = buildIDVal
			commits[i].OverallStatus = statusVal
			if buildIDVal != "" {
				commits[i].Statuses = []CommitStatus{
					{
						BuildID: buildIDVal,
						Status:  statusVal,
					},
				}
			}
		}
	}
}

// GetCommitHistory handles GET /api/repos/{owner}/{repo}/commits/{branch}
func (h *APIHandler) GetCommitHistory(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	branch := chi.URLParam(r, "branch")

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	limit := 50
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if limit > 200 {
		limit = 200
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	args := []string{"--git-dir", localRepoPath, "log", "-n", strconv.Itoa(limit)}
	if offset > 0 {
		args = append(args, "--skip", strconv.Itoa(offset))
	}
	args = append(args, `--format=%H|%an|%ae|%ad|%s`, branch)
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
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

// GetCommitDiff handles GET /api/repos/{owner}/{repo}/commit/{sha}
func (h *APIHandler) GetCommitDiff(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	cmd := exec.Command("git", "--git-dir", localRepoPath, "show", sha)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		http.Error(w, "Git error: "+stderr.String(), http.StatusInternalServerError)
		return
	}

	var statusVal, buildIDVal string
	statusDocs, err := h.FirestoreClient.Collection("commit_statuses").
		Where("owner", "==", owner).
		Where("repo", "==", repo).
		Where("sha", "==", sha).
		Documents(r.Context()).GetAll()
	if err == nil && len(statusDocs) > 0 {
		var latestCreated time.Time
		for _, doc := range statusDocs {
			var data map[string]interface{}
			if err := doc.DataTo(&data); err == nil {
				var created time.Time
				if t, ok := data["createdAt"].(time.Time); ok {
					created = t
				}
				if created.After(latestCreated) || latestCreated.IsZero() {
					latestCreated = created
					statusVal, _ = data["status"].(string)
					buildIDVal, _ = data["buildId"].(string)
				}
			}
		}
	}

	resp := map[string]interface{}{
		"rawDiff": stdout.String(),
		"status":  statusVal,
		"buildId": buildIDVal,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// TreeEntry represents an entry inside a git repository tree.
type TreeEntry struct {
	Mode string      `json:"mode"`
	Type string      `json:"type"`
	SHA  string      `json:"sha"`
	Size interface{} `json:"size"`
	Name string      `json:"name"`
	Path string      `json:"path"`
}

// GetTree handles GET /api/repos/{owner}/{repo}/tree/{branch} and GET /api/repos/{owner}/{repo}/tree/{branch}/*
func (h *APIHandler) GetTree(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	branch := chi.URLParam(r, "branch")
	path := chi.URLParam(r, "*")
	path = strings.TrimPrefix(path, "/")

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	var target string
	if path == "" {
		target = branch
	} else {
		target = branch + ":" + path
	}

	cmd := exec.Command("git", "--git-dir", localRepoPath, "ls-tree", "-l", "--full-name", target)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errStr := strings.ToLower(stderr.String())
		if strings.Contains(errStr, "not a valid object name") || strings.Contains(errStr, "does not exist") || strings.Contains(errStr, "bad default revision") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, "Git error: "+stderr.String(), http.StatusInternalServerError)
		return
	}

	var tree []TreeEntry
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		left := parts[0]
		p := parts[1]

		fields := strings.Fields(left)
		if len(fields) < 4 {
			continue
		}
		mode := fields[0]
		typ := fields[1]
		sha := fields[2]
		sizeStr := fields[3]

		var sizeVal interface{} = sizeStr
		if s, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
			sizeVal = s
		}

		name := filepath.Base(p)
		entryPath := p
		if path != "" {
			entryPath = strings.Trim(path, "/") + "/" + strings.TrimPrefix(p, "/")
		}
		tree = append(tree, TreeEntry{
			Mode: mode,
			Type: typ,
			SHA:  sha,
			Size: sizeVal,
			Name: name,
			Path: entryPath,
		})
	}
	if tree == nil {
		tree = []TreeEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tree)
}

// GetBlob handles GET /api/repos/{owner}/{repo}/blob/{branch}/*
func (h *APIHandler) GetBlob(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	branch := chi.URLParam(r, "branch")
	path := chi.URLParam(r, "*")
	path = strings.TrimPrefix(path, "/")

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	target := branch + ":" + path
	cmd := exec.Command("git", "--git-dir", localRepoPath, "show", target)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errStr := stderr.String()
		if strings.Contains(errStr, "exists but is a commit") || strings.Contains(errStr, "does not exist") || strings.Contains(errStr, "Not a valid object name") {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Git error: "+errStr, http.StatusInternalServerError)
		return
	}

	ext := strings.ToLower(filepath.Ext(path))
	contentType := "text/plain; charset=utf-8"
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	case ".ico":
		contentType = "image/x-icon"
	case ".svg":
		contentType = "image/svg+xml"
	case ".pdf":
		contentType = "application/pdf"
	}

	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(stdout.Bytes())
}

// GetConfig handles GET /api/config
func (h *APIHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	devMode := false
	if dm := os.Getenv("DEV_MODE"); dm != "" {
		if b, err := strconv.ParseBool(dm); err == nil {
			devMode = b
		}
	} else if h.AuthHandler != nil {
		devMode = h.AuthHandler.DevMode
	}

	firebaseProjectID := os.Getenv("FIREBASE_PROJECT_ID")
	if firebaseProjectID == "" {
		firebaseProjectID = os.Getenv("PROJECT_ID")
	}
	if firebaseProjectID == "" {
		firebaseProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}

	firebaseStorageBucket := os.Getenv("FIREBASE_STORAGE_BUCKET")
	if firebaseStorageBucket == "" {
		firebaseStorageBucket = os.Getenv("GCS_BUCKET")
	}

	firebaseConfig := map[string]string{
		"apiKey":            os.Getenv("FIREBASE_API_KEY"),
		"authDomain":        os.Getenv("FIREBASE_AUTH_DOMAIN"),
		"projectId":         firebaseProjectID,
		"storageBucket":     firebaseStorageBucket,
		"messagingSenderId": os.Getenv("FIREBASE_MESSAGING_SENDER_ID"),
		"appId":             os.Getenv("FIREBASE_APP_ID"),
	}

	resp := map[string]interface{}{
		"devMode":  devMode,
		"firebase": firebaseConfig,
		"gitUrl":   os.Getenv("GIT_URL"),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
