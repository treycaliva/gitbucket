package v3

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

// GetRepo handles GET /api/v3/repos/{owner}/{repo}.
//
// Authorization: requires `metadata: read` permission.
func (h *V3Handler) GetRepo(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "metadata", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	meta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		apps.WriteError(w, err)
		return
	}
	if meta == nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}

	apps.WriteJSON(w, http.StatusOK, v3fmt.RepositoryFromMap(meta, h.URLs))
}
