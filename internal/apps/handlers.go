// internal/apps/handlers.go
package apps

import (
	"encoding/json"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	FS    *firestore.Client
	Store SecretStore
	JWT   *JWTVerifier
}

func NewHandler(fs *firestore.Client, store SecretStore, jwt *JWTVerifier) *Handler {
	return &Handler{FS: fs, Store: store, JWT: jwt}
}

// --- Handlers --------------------------------------------------------------

func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	WriteJSON(w, 200, appJSON(app))
}

func (h *Handler) ListInstallations(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	list, err := ListInstallationsForApp(r.Context(), h.FS, app.AppID)
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, i := range list {
		out = append(out, installationJSON(i))
	}
	WriteJSON(w, 200, out)
}

func (h *Handler) GetInstallation(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	id := chi.URLParam(r, "installation_id")
	inst, err := GetInstallationForApp(r.Context(), h.FS, id, app.AppID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if inst == nil {
		WriteError(w, ErrNotFound)
		return
	}
	WriteJSON(w, 200, installationJSON(inst))
}

func (h *Handler) CreateInstallationAccessToken(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	id := chi.URLParam(r, "installation_id")
	inst, err := GetInstallationForApp(r.Context(), h.FS, id, app.AppID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if inst == nil {
		WriteError(w, ErrNotFound)
		return
	}

	var req struct {
		Permissions   map[string]string `json:"permissions"`
		RepositoryIDs []string          `json:"repository_ids"`
	}
	// Body is optional. Ignore decode errors for empty bodies.
	_ = json.NewDecoder(r.Body).Decode(&req)

	mintReq := MintRequest{}
	if len(req.Permissions) > 0 {
		mintReq.Permissions = Permissions{}
		for k, v := range req.Permissions {
			mintReq.Permissions[k] = ParsePermissionLevel(v)
		}
	}
	if len(req.RepositoryIDs) > 0 {
		mintReq.RepositoryIDs = req.RepositoryIDs
	}

	out, err := MintInstallationToken(r.Context(), h.FS, inst, mintReq)
	if err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}

	WriteJSON(w, 201, map[string]interface{}{
		"token":                out.Plaintext,
		"expires_at":           out.Record.ExpiresAt.UTC().Format(time.RFC3339),
		"permissions":          permissionsJSON(out.Record.Permissions),
		"repository_selection": inst.RepositorySelection,
		"single_file":          nil,
		// repositories array is left empty in Plan 1; populated in Plan 2 when
		// the repository formatter exists. Apps using repository_selection ==
		// "all" don't strictly need it, and "selected" installations get an
		// empty list (acceptable for MVP).
		"repositories": []interface{}{},
	})
}

// --- JSON shape helpers (lightweight; full formatters live in Plan 2) -----

func appJSON(a *App) map[string]interface{} {
	return map[string]interface{}{
		"id":          a.AppID,
		"slug":        a.Slug,
		"name":        a.Name,
		"owner":       map[string]interface{}{"login": a.OwnerAccount.ID, "type": string(a.OwnerAccount.Type)},
		"client_id":   a.ClientID,
		"permissions": permissionsJSON(a.DefaultPermissions),
		"events":      a.DefaultEvents,
		"created_at":  a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func installationJSON(i *Installation) map[string]interface{} {
	return map[string]interface{}{
		"id":                   i.InstallationID,
		"app_id":               i.AppID,
		"account":              map[string]interface{}{"login": i.Account.ID, "type": string(i.Account.Type)},
		"repository_selection": i.RepositorySelection,
		"permissions":          permissionsJSON(i.Permissions),
		"events":               i.Events,
		"created_at":           i.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func permissionsJSON(p Permissions) map[string]string {
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = v.String()
	}
	return out
}
