package apps

import (
	"encoding/json"
	"net/http"
)

// CreateUserInstallation handles POST /api/v3/user/installations. The
// authenticated user (Firebase web auth) installs an App onto their account.
//
// Request body:
//
//	{
//	  "app_id": "<app_id>",
//	  "repository_selection": "all" | "selected",
//	  "repository_ids": ["<owner>_<repo>", ...]   // required when selection == "selected"
//	}
func (h *Handler) CreateUserInstallation(w http.ResponseWriter, r *http.Request) {
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}

	var req struct {
		AppID               string   `json:"app_id"`
		RepositorySelection string   `json:"repository_selection"`
		RepositoryIDs       []string `json:"repository_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}
	if req.AppID == "" {
		WriteError(w, ErrUnprocessable)
		return
	}

	app, err := GetApp(r.Context(), h.FS, req.AppID)
	if err != nil || app == nil {
		WriteError(w, ErrNotFound)
		return
	}

	inst, err := CreateInstallation(r.Context(), h.FS, CreateInstallationRequest{
		AppID:               app.AppID,
		Account:             AccountRef{ID: uid, Type: AccountTypeUser},
		RepositorySelection: req.RepositorySelection,
		RepositoryIDs:       req.RepositoryIDs,
		Permissions:         app.DefaultPermissions,
		Events:              app.DefaultEvents,
	})
	if err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}

	// Fire installation:created event (best-effort; Fire is a no-op when
	// Events is zero-valued, so existing tests don't break).
	Fire(r.Context(), h.Events, InstallationPayload{
		Action:  "created",
		AppID:   app.AppID,
		Account: AccountRef{ID: uid, Type: AccountTypeUser},
		Sender:  SenderRef{Login: uid, Type: "User"},
	})

	WriteJSON(w, http.StatusCreated, installationJSON(inst))
}
