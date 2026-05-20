package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

// reviewRequest is the body of POST /api/repos/{owner}/{repo}/pulls/{number}/reviews.
type reviewRequest struct {
	State string `json:"state"` // approved | changes_requested | commented
	Body  string `json:"body"`
}

func isValidReviewState(s string) bool {
	switch s {
	case "approved", "changes_requested", "commented":
		return true
	}
	return false
}

// SubmitReviewHandler handles POST /api/repos/{owner}/{repo}/pulls/{number}/reviews.
// Requires authentication + read access on the repo. The PR author cannot
// review their own PR. Re-submission by the same user replaces the prior review.
func (h *APIHandler) SubmitReviewHandler(w http.ResponseWriter, r *http.Request) {
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
	if uid == "" || username == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !isValidReviewState(req.State) {
		http.Error(w, "state must be approved|changes_requested|commented", http.StatusBadRequest)
		return
	}

	// Authorize read access on the repo.
	if _, _, ok := h.authorizeGitRead(&w, r, owner, repo); !ok {
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

	// PR author cannot review their own PR.
	if pr.AuthorUID == uid {
		http.Error(w, "PR authors cannot review their own pull request", http.StatusForbidden)
		return
	}

	rv := db.Review{
		UID:      uid,
		Username: username,
		State:    req.State,
		Body:     req.Body,
	}
	if err := db.UpsertReview(r.Context(), h.FirestoreClient, owner, repo, number, rv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListReviewsHandler handles GET /api/repos/{owner}/{repo}/pulls/{number}/reviews.
func (h *APIHandler) ListReviewsHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	if _, _, ok := h.authorizeGitRead(&w, r, owner, repo); !ok {
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

	reviews, err := db.ListReviews(r.Context(), h.FirestoreClient, owner, repo, number)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reviews)
}
