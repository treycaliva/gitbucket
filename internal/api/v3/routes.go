package v3

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps"
)

// RegisterV3Routes mounts /api/v3/* endpoints behind the installation-token
// middleware. The four /api/v3/app/* endpoints are mounted separately by
// apps.RegisterRoutes (JWT-authed, different middleware).
func RegisterV3Routes(r chi.Router, h *V3Handler) {
	r.Route("/api/v3", func(r chi.Router) {
		r.Use(apps.RequireInstallationToken(h.FirestoreClient))

		// Skeleton smoke endpoint — used by routing tests to prove the
		// middleware mounts correctly.
		r.Get("/_ping", func(w http.ResponseWriter, r *http.Request) {
			ic := apps.InstallationContextFrom(r.Context())
			if ic == nil {
				apps.WriteError(w, apps.ErrUnauthorized)
				return
			}
			apps.WriteJSON(w, http.StatusOK, map[string]string{
				"installation_id": ic.InstallationID,
			})
		})

		// Real endpoints.
		r.Get("/repos/{owner}/{repo}", h.GetRepo)
		r.Get("/repos/{owner}/{repo}/contents/*", h.GetContents)
	})
}
