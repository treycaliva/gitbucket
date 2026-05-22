// internal/apps/httpio.go
package apps

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Sentinel errors mapped to GitHub-shape HTTP responses by WriteError.
var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrNotFound      = errors.New("not found")
	ErrUnprocessable = errors.New("unprocessable")
)

// PermissionError is returned by RequirePerm. WriteError maps it to 403.
type PermissionError struct {
	Scope string
	Need  PermissionLevel
}

func (e *PermissionError) Error() string {
	return "permission required: " + e.Scope + ":" + e.Need.String()
}

func WriteJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache")
	w.Header().Set("X-GitHub-Media-Type", "github.v3; format=json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, err error) {
	type body struct {
		Message          string `json:"message"`
		DocumentationURL string `json:"documentation_url"`
	}
	docURL := "https://docs.github.com/rest"

	switch {
	case errors.Is(err, ErrUnauthorized):
		WriteJSON(w, http.StatusUnauthorized, body{Message: "Bad credentials", DocumentationURL: docURL})
	case errors.Is(err, ErrForbidden):
		WriteJSON(w, http.StatusForbidden, body{Message: "Forbidden", DocumentationURL: docURL})
	case errors.Is(err, ErrNotFound):
		WriteJSON(w, http.StatusNotFound, body{Message: "Not Found", DocumentationURL: docURL})
	case errors.Is(err, ErrUnprocessable):
		WriteJSON(w, http.StatusUnprocessableEntity, body{Message: err.Error(), DocumentationURL: docURL})
	default:
		var perm *PermissionError
		if errors.As(err, &perm) {
			WriteJSON(w, http.StatusForbidden, body{
				Message:          "Resource not accessible by integration",
				DocumentationURL: docURL,
			})
			return
		}
		WriteJSON(w, http.StatusInternalServerError, body{Message: "Internal server error", DocumentationURL: docURL})
	}
}
