package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

type collaboratorAddReq struct {
	Username string `json:"username"`
}

// ListCollaboratorsHandler handles GET /api/repos/{owner}/{repo}/collaborators.
// Requires read access to the repo.
func (h *APIHandler) ListCollaboratorsHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("collaborators: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}
	meta := db.MapToRepositoryMetadata(repoMeta)

	uid := auth.GetUID(r.Context())
	if !auth.CanRead(meta, uid) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	list, err := db.ListCollaborators(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []db.Collaborator{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// AddCollaboratorHandler handles POST /api/repos/{owner}/{repo}/collaborators.
// Owner only.
func (h *APIHandler) AddCollaboratorHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("collaborators: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}
	meta := db.MapToRepositoryMetadata(repoMeta)
	if meta.OwnerUID != uid {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req collaboratorAddReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if !usernameRegex.MatchString(req.Username) {
		http.Error(w, "Invalid username format", http.StatusBadRequest)
		return
	}

	targetUID, err := db.GetUidByUsername(r.Context(), h.FirestoreClient, req.Username)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if targetUID == meta.OwnerUID {
		http.Error(w, "Owner is implicitly a collaborator", http.StatusBadRequest)
		return
	}

	if err := db.AddCollaborator(r.Context(), h.FirestoreClient, owner, repo, targetUID, req.Username, uid); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveCollaboratorHandler handles DELETE /api/repos/{owner}/{repo}/collaborators/{username}.
// Owner only.
func (h *APIHandler) RemoveCollaboratorHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	username := chi.URLParam(r, "username")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("collaborators: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}
	meta := db.MapToRepositoryMetadata(repoMeta)
	if meta.OwnerUID != uid {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := db.RemoveCollaborator(r.Context(), h.FirestoreClient, owner, repo, username); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
