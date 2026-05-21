package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"
	"google.golang.org/api/idtoken"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
	"gitbucket/internal/sync"
)

// GetSyncConfig returns the repository's GitHub sync configuration.
func (h *APIHandler) GetSyncConfig(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	metaMap, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, "Failed to fetch repository: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if metaMap == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}
	meta := db.MapToRepositoryMetadata(metaMap)

	uid := auth.GetUID(r.Context())
	if meta.OwnerUID != uid {
		http.Error(w, "Forbidden: only repository owner can access sync configuration", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"config": map[string]interface{}{
			"enabled":      meta.GitHubSync.Enabled,
			"githubRepo":   meta.GitHubSync.GitHubRepo,
			"direction":    meta.GitHubSync.Direction,
			"syncStatus":   meta.GitHubSync.SyncStatus,
			"lastSyncedAt": meta.GitHubSync.LastSyncedAt,
			"errorMessage": meta.GitHubSync.ErrorMessage,
		},
	})
}

// UpdateSyncConfig updates the repository's GitHub sync configuration and token.
func (h *APIHandler) UpdateSyncConfig(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	metaMap, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, "Failed to fetch repository: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if metaMap == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}
	meta := db.MapToRepositoryMetadata(metaMap)

	uid := auth.GetUID(r.Context())
	if meta.OwnerUID != uid {
		http.Error(w, "Forbidden: only repository owner can configure sync", http.StatusForbidden)
		return
	}

	var req struct {
		Enabled    bool   `json:"enabled"`
		GitHubRepo string `json:"githubRepo"`
		Direction  string `json:"direction"`
		Token      string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Validate direction
	if req.Direction != "mirror-to-github" && req.Direction != "mirror-to-gitbucket" && req.Direction != "bidirectional" {
		http.Error(w, "Invalid direction: must be 'mirror-to-github', 'mirror-to-gitbucket', or 'bidirectional'", http.StatusBadRequest)
		return
	}

	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := h.FirestoreClient.Collection("repositories").Doc(repoId)

	// If token is provided, encrypt and save it in a subcollection document secrets/github
	if req.Token != "" {
		encToken, encDEK, nonce, err := h.KMSClient.EncryptSecret(r.Context(), req.Token)
		if err != nil {
			http.Error(w, "Failed to encrypt GitHub token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		secretRef := repoRef.Collection("secrets").Doc("github")
		_, err = secretRef.Set(r.Context(), map[string]interface{}{
			"encryptedToken": encToken,
			"encryptedDEK":   encDEK,
			"nonce":          nonce,
			"authMethod":     "pat",
			"updatedAt":      firestore.ServerTimestamp,
			"updatedBy":      uid,
		})
		if err != nil {
			http.Error(w, "Failed to save encrypted token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[Sync Config] Token updated successfully for repo: %s", repoId)
	}

	// Update configuration settings in repository document
	updates := map[string]interface{}{
		"githubSync.enabled":    req.Enabled,
		"githubSync.githubRepo": req.GitHubRepo,
		"githubSync.direction":  req.Direction,
	}

	// Set initial status to synced if it was not enabled before
	if !meta.GitHubSync.Enabled && req.Enabled {
		updates["githubSync.syncStatus"] = "synced"
		updates["githubSync.errorMessage"] = ""
	}

	err = db.UpdateRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo, updates)
	if err != nil {
		http.Error(w, "Failed to update sync config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch updated metadata to return
	updatedMetaMap, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, "Failed to fetch updated metadata", http.StatusInternalServerError)
		return
	}
	updatedMeta := db.MapToRepositoryMetadata(updatedMetaMap)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"config": map[string]interface{}{
			"enabled":      updatedMeta.GitHubSync.Enabled,
			"githubRepo":   updatedMeta.GitHubSync.GitHubRepo,
			"direction":    updatedMeta.GitHubSync.Direction,
			"syncStatus":   updatedMeta.GitHubSync.SyncStatus,
			"lastSyncedAt": updatedMeta.GitHubSync.LastSyncedAt,
		},
	})
}

// ResolveSyncConflict initiates a force-sync task to resolve divergent branches.
func (h *APIHandler) ResolveSyncConflict(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	metaMap, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, "Failed to fetch repository: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if metaMap == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}
	meta := db.MapToRepositoryMetadata(metaMap)

	uid := auth.GetUID(r.Context())
	if meta.OwnerUID != uid {
		http.Error(w, "Forbidden: only repository owner can resolve conflicts", http.StatusForbidden)
		return
	}

	var req struct {
		Strategy string `json:"strategy"` // "force-gitbucket" or "force-github"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if req.Strategy != "force-gitbucket" && req.Strategy != "force-github" {
		http.Error(w, "Invalid strategy: must be 'force-gitbucket' or 'force-github'", http.StatusBadRequest)
		return
	}

	// Trigger async sync with force strategy
	h.TriggerSyncTask(owner, repo, meta.GitHubSync.Direction, true, req.Strategy)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Force-sync task enqueued successfully",
	})
}

// HandleSyncTask executes the Git synchronization synchronously. Used by Google Cloud Workflows.
func (h *APIHandler) HandleSyncTask(w http.ResponseWriter, r *http.Request) {
	// Verify OIDC token unless in DevMode or if Authorization header is missing in DevMode
	authHeader := r.Header.Get("Authorization")
	if !h.AuthHandler.DevMode {
		var token string
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = authHeader[7:]
		} else if strings.HasPrefix(authHeader, "bearer ") {
			token = authHeader[7:]
		}
		if token == "" {
			http.Error(w, "Unauthorized: missing bearer token", http.StatusUnauthorized)
			return
		}

		// Validate OIDC ID token
		audience := "https://" + r.Host + "/api/tasks/sync-github"
		payload, err := idtoken.Validate(r.Context(), token, audience)
		if err != nil {
			// Fallback audience (Host only)
			fallbackAud := "https://" + r.Host
			payload, err = idtoken.Validate(r.Context(), token, fallbackAud)
			if err != nil {
				log.Printf("[OIDC Verify] Token validation failed: %v", err)
				http.Error(w, "Unauthorized: invalid OIDC token", http.StatusUnauthorized)
				return
			}
		}

		// Verify email claim belongs to Google Cloud service account
		email, _ := payload.Claims["email"].(string)
		if !strings.HasSuffix(email, ".gserviceaccount.com") {
			log.Printf("[OIDC Verify] Unauthorized service account: %s", email)
			http.Error(w, "Forbidden: unauthorized service account", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Owner         string `json:"owner"`
		Repo          string `json:"repo"`
		Direction     string `json:"direction"`
		Force         bool   `json:"force"`
		ForceStrategy string `json:"forceStrategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if req.Owner == "" || req.Repo == "" {
		http.Error(w, "Missing owner or repo", http.StatusBadRequest)
		return
	}

	res, err := sync.ExecuteSync(r.Context(), h.FirestoreClient, h.StorageClient, h.BucketName, h.LocalReposRoot, h.KMSClient, req.Owner, req.Repo, req.Direction, req.Force, req.ForceStrategy)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "lock is active") || strings.Contains(errMsg, "failed to acquire leased lock") {
			log.Printf("[Sync Task Conflict] %v", err)
			http.Error(w, errMsg, http.StatusConflict) // Return 409 for Workflows retry
			return
		}
		log.Printf("[Sync Task Error] %v", err)
		http.Error(w, errMsg, http.StatusInternalServerError)
		return
	}

	// Update Repository status in Firestore
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(req.Owner), strings.ToLower(req.Repo))
	updates := []firestore.Update{
		{Path: "githubSync.syncStatus", Value: res.Status},
		{Path: "githubSync.errorMessage", Value: res.ErrorMessage},
		{Path: "githubSync.lastSyncedAt", Value: firestore.ServerTimestamp},
	}
	if res.GitbucketSha != "" {
		updates = append(updates, firestore.Update{Path: "githubSync.gitbucketSha", Value: res.GitbucketSha})
	}
	if res.GithubSha != "" {
		updates = append(updates, firestore.Update{Path: "githubSync.githubSha", Value: res.GithubSha})
	}
	_, _ = h.FirestoreClient.Collection("repositories").Doc(repoId).Update(r.Context(), updates)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// WaitSync blocks the request until the target commit SHA is successfully synchronized.
func (h *APIHandler) WaitSync(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	targetSha := r.URL.Query().Get("commit")
	if targetSha == "" {
		http.Error(w, "Missing commit query parameter", http.StatusBadRequest)
		return
	}

	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := h.FirestoreClient.Collection("repositories").Doc(repoId)

	// Wait up to 30 seconds
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	it := repoRef.Snapshots(ctx)
	defer it.Stop()

	for {
		snap, err := it.Next()
		if err != nil {
			if ctx.Err() != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte(`{"status":"timeout","error":"Timeout waiting for sync"}`))
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if !snap.Exists() {
			http.Error(w, "Repository not found", http.StatusNotFound)
			return
		}

		var data map[string]interface{}
		if err := snap.DataTo(&data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if ghSyncVal, ok := data["githubSync"]; ok {
			if ghSync, ok := ghSyncVal.(map[string]interface{}); ok {
				status, _ := ghSync["syncStatus"].(string)
				if status == "conflict" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"status":"conflict","error":"Sync conflict detected"}`))
					return
				}

				gitbucketSha, _ := ghSync["gitbucketSha"].(string)
				githubSha, _ := ghSync["githubSha"].(string)
				if gitbucketSha == targetSha || githubSha == targetSha {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"status":"synced"}`))
					return
				}
			}
		}
	}
}

// TriggerSyncTask invokes the sync task asynchronously in a background goroutine.
func (h *APIHandler) TriggerSyncTask(owner, repo, direction string, force bool, forceStrategy string) {
	log.Printf("[Sync Trigger] Scheduling sync task for %s/%s in background (direction=%s)...", owner, repo, direction)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
		repoRef := h.FirestoreClient.Collection("repositories").Doc(repoId)

		// Update repository status to "syncing"
		_, _ = repoRef.Update(ctx, []firestore.Update{
			{Path: "githubSync.syncStatus", Value: "syncing"},
			{Path: "githubSync.errorMessage", Value: ""},
		})

		res, err := sync.ExecuteSync(ctx, h.FirestoreClient, h.StorageClient, h.BucketName, h.LocalReposRoot, h.KMSClient, owner, repo, direction, force, forceStrategy)

		var updates []firestore.Update
		if err != nil {
			log.Printf("[Sync Background Error] Sync failed for %s/%s: %v", owner, repo, err)
			updates = []firestore.Update{
				{Path: "githubSync.syncStatus", Value: "error"},
				{Path: "githubSync.errorMessage", Value: err.Error()},
				{Path: "githubSync.lastSyncedAt", Value: firestore.ServerTimestamp},
			}
		} else {
			log.Printf("[Sync Background Success] Sync complete for %s/%s. Status: %s", owner, repo, res.Status)
			updates = []firestore.Update{
				{Path: "githubSync.syncStatus", Value: res.Status},
				{Path: "githubSync.errorMessage", Value: res.ErrorMessage},
				{Path: "githubSync.lastSyncedAt", Value: firestore.ServerTimestamp},
			}
			if res.GitbucketSha != "" {
				updates = append(updates, firestore.Update{Path: "githubSync.gitbucketSha", Value: res.GitbucketSha})
			}
			if res.GithubSha != "" {
				updates = append(updates, firestore.Update{Path: "githubSync.githubSha", Value: res.GithubSha})
			}
		}
		_, _ = repoRef.Update(ctx, updates)
	}()
}
