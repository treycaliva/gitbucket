package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
	"gitbucket/internal/git"
)

// validatePattern checks that a branch-protection pattern is non-empty,
// within length bounds, and well-formed per filepath.Match semantics.
func validatePattern(p string) error {
	if p == "" || len(p) > 200 {
		return fmt.Errorf("invalid pattern length")
	}
	if _, err := filepath.Match(p, "test"); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	return nil
}

// ListBranchProtectionHandler handles GET /api/repos/{owner}/{repo}/branch-protection.
// Requires read access.
func (h *APIHandler) ListBranchProtectionHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("branch_protection: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
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

	rules, err := db.ListRules(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rules == nil {
		rules = []git.Rule{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rules)
}

// CreateBranchProtectionHandler handles POST /api/repos/{owner}/{repo}/branch-protection.
// Owner only.
func (h *APIHandler) CreateBranchProtectionHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("branch_protection: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
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

	var rule git.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if err := validatePattern(rule.Pattern); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	created, err := db.CreateRule(r.Context(), h.FirestoreClient, owner, repo, rule)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(created)
}

// UpdateBranchProtectionHandler handles PUT /api/repos/{owner}/{repo}/branch-protection/{ruleId}.
// Owner only. Full replace.
func (h *APIHandler) UpdateBranchProtectionHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	ruleId := chi.URLParam(r, "ruleId")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("branch_protection: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
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

	existing, err := db.GetRule(r.Context(), h.FirestoreClient, owner, repo, ruleId)
	if err != nil || existing == nil {
		http.Error(w, "Rule not found", http.StatusNotFound)
		return
	}

	var rule git.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if err := validatePattern(rule.Pattern); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := db.UpdateRule(r.Context(), h.FirestoreClient, owner, repo, rule); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteBranchProtectionHandler handles DELETE /api/repos/{owner}/{repo}/branch-protection/{ruleId}.
// Owner only.
func (h *APIHandler) DeleteBranchProtectionHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	ruleId := chi.URLParam(r, "ruleId")

	uid := auth.GetUID(r.Context())
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("branch_protection: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
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

	if err := db.DeleteRule(r.Context(), h.FirestoreClient, owner, repo, ruleId); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
