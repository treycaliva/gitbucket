// internal/apps/routes.go
package apps

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the /api/v3/app/* routes (JWT-authed). The
// installation-token-authed endpoints (e.g. repos, issues, pulls) will be
// mounted by Plan 2 via a separate RegisterV3Routes function.
func RegisterRoutes(r chi.Router, h *Handler) {
	r.Route("/api/v3/app", func(r chi.Router) {
		r.Use(RequireAppJWT(h.JWT))
		r.Get("/", h.GetApp)
		r.Get("/installations", h.ListInstallations)
		r.Get("/installations/{installation_id}", h.GetInstallation)
		r.Post("/installations/{installation_id}/access_tokens", h.CreateInstallationAccessToken)
	})
}
