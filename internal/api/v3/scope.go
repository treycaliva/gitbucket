package v3

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

// RequireRepoScope restricts /repos/{owner}/{repo} routes to repositories the
// authenticated installation is actually installed on: the repo must exist,
// belong to the installation's account, and — when the token was minted for a
// repository subset — be in that subset. Permission scopes (RequirePerm) say
// what an installation may do; this middleware says where it may do it.
//
// All rejections return 404 in GitHub shape, matching github.com behavior for
// repositories a token cannot see, so probing does not reveal which repos exist.
func RequireRepoScope(h *V3Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ic := apps.InstallationContextFrom(r.Context())
			if ic == nil {
				apps.WriteError(w, apps.ErrUnauthorized)
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
			ownerUID, _ := meta["ownerUid"].(string)
			if ownerUID == "" || ownerUID != ic.Account.ID {
				apps.WriteError(w, apps.ErrNotFound)
				return
			}

			// Empty RepositoryIDs means the token inherits repository_selection
			// "all" (MintInstallationToken copies the installation's IDs when a
			// "selected" installation mints, and validates any narrowing).
			if len(ic.RepositoryIDs) > 0 {
				repoID := strings.ToLower(owner) + "_" + strings.ToLower(repo)
				granted := false
				for _, id := range ic.RepositoryIDs {
					if strings.ToLower(id) == repoID {
						granted = true
						break
					}
				}
				if !granted {
					apps.WriteError(w, apps.ErrNotFound)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
