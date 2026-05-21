package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
	"gitbucket/internal/gcs"
	gitpkg "gitbucket/internal/git"
)

// getUpdatedAtMs extracts millisecond timestamp from repo metadata
func getUpdatedAtMs(meta map[string]interface{}) int64 {
	if meta == nil {
		return 0
	}
	tVal, ok := meta["updatedAt"]
	if !ok {
		return 0
	}
	switch t := tVal.(type) {
	case time.Time:
		return t.UnixNano() / 1e6
	case *time.Time:
		if t != nil {
			return t.UnixNano() / 1e6
		}
	}
	return 0
}

// getLocalSyncTimestamp reads the local last_sync_timestamp file
func getLocalSyncTimestamp(localRepoPath string) int64 {
	tsPath := filepath.Join(localRepoPath, "last_sync_timestamp")
	data, err := os.ReadFile(tsPath)
	if err != nil {
		return 0
	}
	val, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// writeLocalSyncTimestamp writes the millisecond timestamp to local last_sync_timestamp file
func writeLocalSyncTimestamp(localRepoPath string, ms int64) error {
	tsPath := filepath.Join(localRepoPath, "last_sync_timestamp")
	err := os.MkdirAll(filepath.Dir(tsPath), 0755)
	if err != nil {
		return err
	}
	return os.WriteFile(tsPath, []byte(strconv.FormatInt(ms, 10)), 0644)
}

// getRepoBranches executes git command to find current list of local branch names
func getRepoBranches(localRepoPath string) []string {
	cmd := exec.Command("git", "--git-dir", localRepoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return []string{"main"}
	}
	var branches []string
	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			branches = append(branches, trimmed)
		}
	}
	if len(branches) == 0 {
		return []string{"main"}
	}
	return branches
}

// findGitHttpBackend locates the git-http-backend executable
func findGitHttpBackend() (string, error) {
	cmd := exec.Command("git", "--exec-path")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		execPath := strings.TrimSpace(out.String())
		backend := filepath.Join(execPath, "git-http-backend")
		if _, err := os.Stat(backend); err == nil {
			return backend, nil
		}
	}

	commonPaths := []string{
		"/usr/lib/git-core/git-http-backend",
		"/usr/libexec/git-core/git-http-backend",
		"/Library/Developer/CommandLineTools/usr/libexec/git-core/git-http-backend",
	}
	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("git-http-backend not found")
}

// HandleGitHTTP handles Smart HTTP protocol requests
func (h *APIHandler) HandleGitHTTP(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoParam := chi.URLParam(r, "repo")
	repo := strings.TrimSuffix(repoParam, ".git")
	wildcard := chi.URLParam(r, "*")

	isWrite := strings.Contains(r.URL.Path, "git-receive-pack") || r.URL.Query().Get("service") == "git-receive-pack"

	log.Printf("[Git HTTP] Request: %s %s (isWrite: %v)", r.Method, r.URL.String(), isWrite)

	// 1. Fetch Repository Metadata
	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("git: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	// 2. Authentication and Authorization Check
	var authenticatedUser string
	var authenticatedUID string

	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if os.Getenv("DEV_MODE") == "true" && tokenStr == "mock_gcb_token" {
			authenticatedUID = "gcb-runner"
			authenticatedUser = "gcb-runner"
		} else {
			gitbucketURL := getGitbucketURL(r)
			audience := fmt.Sprintf("%s/repos/%s/%s", gitbucketURL, owner, repo)
			payload, err := VerifyCloudBuildOIDCToken(r.Context(), tokenStr, audience)
			if err == nil && payload != nil {
				authenticatedUID = "gcb-runner"
				authenticatedUser = "gcb-runner"
			} else {
				log.Printf("[Git HTTP] OIDC token verification failed: %v", err)
			}
		}
	} else {
		username, pat, ok := r.BasicAuth()
		if ok {
			verifiedUID, verifiedUsername, err := db.VerifyPAT(r.Context(), h.FirestoreClient, pat)
			if err == nil {
				if strings.EqualFold(verifiedUsername, username) {
					authenticatedUser = verifiedUsername
					authenticatedUID = verifiedUID
				}
			}
		}
	}

	meta := db.MapToRepositoryMetadata(repoMeta)
	if isWrite {
		if !auth.CanPush(meta, authenticatedUID) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitBucket"`)
			http.Error(w, "Unauthorized: push access required.", http.StatusUnauthorized)
			return
		}
	} else {
		if authenticatedUID != "gcb-runner" && !auth.CanRead(meta, authenticatedUID) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitBucket"`)
			http.Error(w, "Unauthorized: read access required.", http.StatusUnauthorized)
			return
		}
	}

	// 3. Locking and Syncing Cache
	var lockToken string
	localRepoPath := filepath.Join(h.LocalReposRoot, strings.ToLower(owner), fmt.Sprintf("%s.git", strings.ToLower(repo)))

	needsSync := getLocalSyncTimestamp(localRepoPath) != getUpdatedAtMs(repoMeta)

	if isWrite || needsSync {
		lockToken, err = db.AcquireLock(r.Context(), h.FirestoreClient, owner, repo, 5*time.Minute, 30*time.Second)
		if err != nil {
			http.Error(w, "Failed to acquire repository lock: "+err.Error(), http.StatusConflict)
			return
		}
	}

	defer func() {
		if lockToken != "" {
			_ = db.ReleaseLock(context.Background(), h.FirestoreClient, owner, repo, lockToken)
		}
	}()

	if needsSync {
		var downloaded bool
		if h.StorageClient != nil && h.BucketName != "" {
			downloaded, err = gcs.DownloadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
			if err != nil {
				http.Error(w, "Failed to download repository: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if !downloaded {
			if err := ensureBareRepo(localRepoPath); err != nil {
				http.Error(w, "Failed to initialize repository: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if err := writeLocalSyncTimestamp(localRepoPath, getUpdatedAtMs(repoMeta)); err != nil {
			http.Error(w, "Failed to write local sync timestamp: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Capture pre-push refs for branch-protection enforcement (only matters for writes).
	var preRefs map[string]string
	if isWrite {
		preRefs = gitpkg.LocalRefs(localRepoPath)
	}

	// 4. CGI Execution of git-http-backend
	backendPath, err := findGitHttpBackend()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wildcard != "" && !strings.HasPrefix(wildcard, "/") {
		wildcard = "/" + wildcard
	}
	pathInfo := "/" + strings.ToLower(owner) + "/" + strings.ToLower(repo) + ".git" + wildcard

	cmd := exec.CommandContext(r.Context(), backendPath)
	cmd.Env = append(os.Environ(),
		"GIT_PROJECT_ROOT="+h.LocalReposRoot,
		"GIT_HTTP_EXPORT_ALL=1",
		"PATH_INFO="+pathInfo,
		"REQUEST_METHOD="+r.Method,
		"QUERY_STRING="+r.URL.RawQuery,
		"CONTENT_TYPE="+r.Header.Get("Content-Type"),
		"REMOTE_USER="+authenticatedUser,
		"REMOTE_ADDR="+r.RemoteAddr,
	)
	cmd.Stdin = r.Body

	stdoutReader, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "Failed to get stdout pipe: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		http.Error(w, "Failed to start git backend: "+err.Error(), http.StatusInternalServerError)
		return
	}

	reader := bufio.NewReader(stdoutReader)
	var statusCode = http.StatusOK
	var headers = make(http.Header)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if strings.ToLower(key) == "status" {
				statusParts := strings.SplitN(val, " ", 2)
				if len(statusParts) > 0 {
					if code, parseErr := strconv.Atoi(statusParts[0]); parseErr == nil {
						statusCode = code
					}
				}
			} else {
				headers.Add(key, val)
			}
		}
	}

	for k, v := range headers {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(statusCode)

	_, _ = io.Copy(w, reader)

	waitErr := cmd.Wait()
	if waitErr != nil {
		log.Printf("[Git HTTP] Warning: git-http-backend exited with error: %v, stderr: %s", waitErr, stderrBuf.String())
	}

	// 5. Post-push actions
	if isWrite && waitErr == nil {
		// 5a. Branch-protection enforcement.
		// The CGI accepted the pack and updated the local refs; before we sync them
		// to GCS/Firestore, evaluate each ref delta against the repo's rules.
		postRefs := gitpkg.LocalRefs(localRepoPath)
		updates := gitpkg.DiffRefs(localRepoPath, preRefs, postRefs)
		if len(updates) > 0 {
			rules, ruleErr := db.ListRules(r.Context(), h.FirestoreClient, owner, repo)
			if ruleErr != nil {
				log.Printf("[Git HTTP] Failed to load branch protection rules: %v (allowing push)", ruleErr)
			} else {
				res := gitpkg.EnforcePush(rules, updates, authenticatedUID)
				if len(res.Rejected) > 0 {
					// Cannot rewrite the HTTP status — the CGI already streamed its response.
					// Block the GCS/Firestore sync so the rejected refs never become
					// the source of truth; drop the local sync timestamp so the next
					// request re-materializes the canonical state from storage.
					for ref, why := range res.Reasons {
						log.Printf("[Git HTTP] REJECTED ref=%s reason=%s", ref, why)
					}
					// Rollback refs locally so the local bare repo matches pre-push state.
					for ref, oldSha := range preRefs {
						cmd := exec.Command("git", "--git-dir", localRepoPath, "update-ref", ref, oldSha)
						if rollbackErr := cmd.Run(); rollbackErr != nil {
							log.Printf("[Git HTTP] Failed to rollback ref %s to %s: %v", ref, oldSha, rollbackErr)
						}
					}
					for ref := range postRefs {
						if _, existed := preRefs[ref]; !existed {
							cmd := exec.Command("git", "--git-dir", localRepoPath, "update-ref", "-d", ref)
							if rollbackErr := cmd.Run(); rollbackErr != nil {
								log.Printf("[Git HTTP] Failed to delete rejected new ref %s: %v", ref, rollbackErr)
							}
						}
					}
					_ = os.Remove(filepath.Join(localRepoPath, "last_sync_timestamp"))
					return
				}
			}
		}

		log.Printf("[Git HTTP] Write operation completed successfully. Archiving to GCS...")
		if h.StorageClient != nil && h.BucketName != "" {
			err = gcs.UploadRepo(r.Context(), h.StorageClient, h.BucketName, owner, repo, h.LocalReposRoot)
			if err != nil {
				log.Printf("Failed to upload repo to GCS: %v", err)
			}
		}

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

			// Trigger Cloud Build if cloudbuild.yaml exists in any updated branch refs
			if len(updates) > 0 {
				for _, u := range updates {
					if u.IsDelete {
						continue
					}
					branch := strings.TrimPrefix(u.RefName, "refs/heads/")
					if branch == u.RefName {
						continue // not refs/heads/
					}
					// Check if cloudbuild.yaml exists in the new commit
					checkCmd := exec.Command("git", "--git-dir", localRepoPath, "cat-file", "-e", fmt.Sprintf("%s:cloudbuild.yaml", u.NewSha))
					if err := checkCmd.Run(); err == nil {
						log.Printf("[Git HTTP] cloudbuild.yaml detected in commit %s (branch %s). Dispatching Cloud Build...", u.NewSha, branch)
						go h.TriggerCloudBuild(owner, repo, branch, u.NewSha)
					}
				}
			}
		}
		log.Printf("[Git HTTP] GCS sync and Firestore metadata update complete!")
	}
}

// LFS-specific types
type LFSObject struct {
	Oid  string `json:"oid"`
	Size int64  `json:"size"`
}

type LFSBatchRequest struct {
	Operation string      `json:"operation"`
	Transfers []string    `json:"transfers,omitempty"`
	Ref       *LFSRef     `json:"ref,omitempty"`
	Objects   []LFSObject `json:"objects"`
}

type LFSRef struct {
	Name string `json:"name"`
}

type LFSAction struct {
	Href      string            `json:"href"`
	Header    map[string]string `json:"header,omitempty"`
	ExpiresIn int               `json:"expires_in,omitempty"`
}

type LFSObjectResponse struct {
	Oid     string                `json:"oid"`
	Size    int64                 `json:"size"`
	Actions map[string]*LFSAction `json:"actions,omitempty"`
	Error   *LFSError             `json:"error,omitempty"`
}

type LFSError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type LFSBatchResponse struct {
	Transfer string              `json:"transfer,omitempty"`
	Objects  []LFSObjectResponse `json:"objects"`
}

func (h *APIHandler) HandleLFSBatch(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoParam := chi.URLParam(r, "repo")
	repo := strings.TrimSuffix(repoParam, ".git")

	// 1. Decode batch request body
	var req LFSBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 2. Fetch repo metadata and verify authorization
	isUpload := req.Operation == "upload"
	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Database error"})
		return
	}
	if repoMeta == nil {
		w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Repository not found"})
		return
	}

	var authenticatedUID string
	username, pat, ok := r.BasicAuth()
	if ok {
		verifiedUID, verifiedUsername, err := db.VerifyPAT(r.Context(), h.FirestoreClient, pat)
		if err == nil {
			if strings.EqualFold(verifiedUsername, username) {
				authenticatedUID = verifiedUID
			}
		}
	}

	meta := db.MapToRepositoryMetadata(repoMeta)
	if isUpload {
		if !auth.CanPush(meta, authenticatedUID) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitBucket"`)
			w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Unauthorized: push access required."})
			return
		}
	} else { // download
		if !auth.CanRead(meta, authenticatedUID) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitBucket"`)
			w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Unauthorized: read access required."})
			return
		}
	}

	// 3. Construct response
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	var headerMap map[string]string
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		headerMap = map[string]string{
			"Authorization": authHeader,
		}
	}

	var objectsResp []LFSObjectResponse
	for _, obj := range req.Objects {
		respObj := LFSObjectResponse{
			Oid:  obj.Oid,
			Size: obj.Size,
		}

		if req.Operation == "upload" {
			respObj.Actions = map[string]*LFSAction{
				"upload": {
					Href:   fmt.Sprintf("%s://%s/r/%s/%s.git/info/lfs/objects/%s", scheme, r.Host, owner, repo, obj.Oid),
					Header: headerMap,
				},
			}
		} else if req.Operation == "download" {
			gcsPath := fmt.Sprintf("repos/%s/%s/lfs/%s", strings.ToLower(owner), strings.ToLower(repo), obj.Oid)
			exists := false
			var attrsErr error
			if h.StorageClient != nil && h.BucketName != "" {
				bucket := h.StorageClient.Bucket(h.BucketName)
				_, attrsErr = bucket.Object(gcsPath).Attrs(r.Context())
				if attrsErr == nil {
					exists = true
				}
			} else {
				localPath := filepath.Join(h.LocalReposRoot, strings.ToLower(owner), strings.ToLower(repo), "lfs", obj.Oid)
				if _, err := os.Stat(localPath); err == nil {
					exists = true
				}
			}
			log.Printf("[LFS Batch Download] Path check: bucket=%s path=%s exists=%v err=%v", h.BucketName, gcsPath, exists, attrsErr)
			if exists {
				respObj.Actions = map[string]*LFSAction{
					"download": {
						Href:   fmt.Sprintf("%s://%s/r/%s/%s.git/info/lfs/objects/%s", scheme, r.Host, owner, repo, obj.Oid),
						Header: headerMap,
					},
				}
			} else {
				respObj.Error = &LFSError{
					Code:    http.StatusNotFound,
					Message: "Object not found",
				}
			}
		}

		objectsResp = append(objectsResp, respObj)
	}

	resp := LFSBatchResponse{
		Transfer: "basic",
		Objects:  objectsResp,
	}

	w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *APIHandler) HandleLFSUpload(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoParam := chi.URLParam(r, "repo")
	repo := strings.TrimSuffix(repoParam, ".git")
	oid := chi.URLParam(r, "oid")

	// 1. Fetch metadata and check ownership
	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("git: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	var authenticatedUID string
	username, pat, ok := r.BasicAuth()
	if ok {
		verifiedUID, verifiedUsername, err := db.VerifyPAT(r.Context(), h.FirestoreClient, pat)
		if err == nil {
			if strings.EqualFold(verifiedUsername, username) {
				authenticatedUID = verifiedUID
			}
		}
	}

	meta := db.MapToRepositoryMetadata(repoMeta)
	if !auth.CanPush(meta, authenticatedUID) {
		w.Header().Set("WWW-Authenticate", `Basic realm="GitBucket"`)
		http.Error(w, "Unauthorized: push access required.", http.StatusUnauthorized)
		return
	}

	// 2. Stream request body to GCS
	if h.StorageClient == nil || h.BucketName == "" {
		localLFSDir := filepath.Join(h.LocalReposRoot, strings.ToLower(owner), strings.ToLower(repo), "lfs")
		if err := os.MkdirAll(localLFSDir, 0755); err != nil {
			http.Error(w, "Failed to create local LFS directory: "+err.Error(), http.StatusInternalServerError)
			return
		}
		localPath := filepath.Join(localLFSDir, oid)
		f, err := os.Create(localPath)
		if err != nil {
			http.Error(w, "Failed to create local LFS file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		if _, err := io.Copy(f, r.Body); err != nil {
			http.Error(w, "Failed to write local LFS file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[LFS Upload] Local disk success: path=%s", localPath)
		w.WriteHeader(http.StatusOK)
		return
	}

	gcsPath := fmt.Sprintf("repos/%s/%s/lfs/%s", strings.ToLower(owner), strings.ToLower(repo), oid)
	log.Printf("[LFS Upload] Writing LFS object: bucket=%s path=%s", h.BucketName, gcsPath)
	bucket := h.StorageClient.Bucket(h.BucketName)
	obj := bucket.Object(gcsPath)
	gcsWriter := obj.NewWriter(r.Context())
	gcsWriter.CacheControl = "no-cache, no-store, must-revalidate"

	if _, err := io.Copy(gcsWriter, r.Body); err != nil {
		_ = gcsWriter.Close()
		http.Error(w, "Failed to write object to GCS: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := gcsWriter.Close(); err != nil {
		http.Error(w, "Failed to close GCS writer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	attrs, err := obj.Attrs(r.Context())
	if err != nil {
		log.Printf("[LFS Upload Verify] FAILED: attrs check failed for path %s: %v", gcsPath, err)
	} else {
		log.Printf("[LFS Upload Verify] SUCCESS: verified path %s size %d", gcsPath, attrs.Size)
	}

	w.WriteHeader(http.StatusOK)
}

// TagInfo is a single tag with its target commit SHA.
type TagInfo struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

// ListTags handles GET /api/repos/{owner}/{repo}/tags
func (h *APIHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	cmd := exec.Command("git", "--git-dir", localRepoPath, "for-each-ref",
		"--sort=-creatordate",
		"--format=%(refname:short)|%(objectname)",
		"refs/tags")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errStr := stderr.String()
		if strings.Contains(errStr, "does not have any commits") || strings.Contains(errStr, "fatal: bad default revision") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, "Git error: "+errStr, http.StatusInternalServerError)
		return
	}

	tags := []TagInfo{}
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		tags = append(tags, TagInfo{Name: parts[0], SHA: parts[1]})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tags)
}

// BranchHeadInfo is the head commit on a branch plus total commit count.
type BranchHeadInfo struct {
	SHA          string `json:"sha"`
	AuthorName   string `json:"authorName"`
	AuthorEmail  string `json:"authorEmail"`
	Date         string `json:"date"`
	Message      string `json:"message"`
	TotalCommits int    `json:"totalCommits"`
}

// GetBranchHead handles GET /api/repos/{owner}/{repo}/refs/{branch}/head
func (h *APIHandler) GetBranchHead(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	branch := chi.URLParam(r, "branch")

	_, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	writeEmpty := func() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(BranchHeadInfo{})
	}

	// Head commit (1 row)
	logCmd := exec.Command("git", "--git-dir", localRepoPath, "log", "-n", "1", `--format=%H|%an|%ae|%ad|%s`, branch)
	var logOut, logErr bytes.Buffer
	logCmd.Stdout = &logOut
	logCmd.Stderr = &logErr
	if err := logCmd.Run(); err != nil {
		errStr := logErr.String()
		if strings.Contains(errStr, "does not have any commits") || strings.Contains(errStr, "fatal: bad default revision") || strings.Contains(errStr, "unknown revision") {
			writeEmpty()
			return
		}
		http.Error(w, "Git error: "+errStr, http.StatusInternalServerError)
		return
	}

	parts := strings.SplitN(strings.TrimSpace(logOut.String()), "|", 5)
	if len(parts) < 5 {
		writeEmpty()
		return
	}

	// Total commit count
	countCmd := exec.Command("git", "--git-dir", localRepoPath, "rev-list", "--count", branch)
	var countOut bytes.Buffer
	countCmd.Stdout = &countOut
	_ = countCmd.Run()
	total, _ := strconv.Atoi(strings.TrimSpace(countOut.String()))

	head := BranchHeadInfo{
		SHA:          parts[0],
		AuthorName:   parts[1],
		AuthorEmail:  parts[2],
		Date:         parts[3],
		Message:      parts[4],
		TotalCommits: total,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(head)
}

func (h *APIHandler) HandleLFSDownload(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoParam := chi.URLParam(r, "repo")
	repo := strings.TrimSuffix(repoParam, ".git")
	oid := chi.URLParam(r, "oid")

	// 1. Fetch metadata and check permissions
	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("git: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	var authenticatedUID string
	username, pat, ok := r.BasicAuth()
	if ok {
		verifiedUID, verifiedUsername, err := db.VerifyPAT(r.Context(), h.FirestoreClient, pat)
		if err == nil {
			if strings.EqualFold(verifiedUsername, username) {
				authenticatedUID = verifiedUID
			}
		}
	}

	meta := db.MapToRepositoryMetadata(repoMeta)
	if !auth.CanRead(meta, authenticatedUID) {
		w.Header().Set("WWW-Authenticate", `Basic realm="GitBucket"`)
		http.Error(w, "Unauthorized: read access required.", http.StatusUnauthorized)
		return
	}

	// 2. Stream object from GCS to client
	if h.StorageClient == nil || h.BucketName == "" {
		localPath := filepath.Join(h.LocalReposRoot, strings.ToLower(owner), strings.ToLower(repo), "lfs", oid)
		info, err := os.Stat(localPath)
		if err != nil {
			http.Error(w, "Object not found locally: "+err.Error(), http.StatusNotFound)
			return
		}
		f, err := os.Open(localPath)
		if err != nil {
			http.Error(w, "Failed to open local LFS file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, f); err != nil {
			log.Printf("Failed to copy local object content: %v", err)
		}
		return
	}

	gcsPath := fmt.Sprintf("repos/%s/%s/lfs/%s", strings.ToLower(owner), strings.ToLower(repo), oid)
	log.Printf("[LFS Download] GCS path: bucket=%s path=%s", h.BucketName, gcsPath)
	bucket := h.StorageClient.Bucket(h.BucketName)
	obj := bucket.Object(gcsPath)

	// Get attributes to determine content length
	attrs, err := obj.Attrs(r.Context())
	if err != nil {
		log.Printf("[LFS Download] obj.Attrs failed for %s: %v", gcsPath, err)
		http.Error(w, "Object not found: "+err.Error(), http.StatusNotFound)
		return
	}

	gcsReader, err := obj.NewReader(r.Context())
	if err != nil {
		http.Error(w, "Failed to read object from GCS: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer gcsReader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(attrs.Size, 10))
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, gcsReader); err != nil {
		log.Printf("Failed to copy object content: %v", err)
	}
}
