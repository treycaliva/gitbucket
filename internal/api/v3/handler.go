// Package v3 implements the GitHub-compatible REST surface (/api/v3/*).
// Handlers in this package authenticate via installation tokens (see
// internal/apps middleware) and shape responses via internal/api/v3/v3fmt.
package v3

import (
	"os"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
)

// V3Handler bundles the dependencies needed by GitHub-shape handlers.
type V3Handler struct {
	FirestoreClient *firestore.Client
	StorageClient   *storage.Client // for repo materialization (GCS → local bare repo)
	URLs            *v3fmt.URLBuilder
	LocalReposRoot  string
	Events          apps.FireDeps // populated by main.go after construction
}

func NewV3Handler(fs *firestore.Client, sc *storage.Client, baseURL string) *V3Handler {
	return &V3Handler{
		FirestoreClient: fs,
		StorageClient:   sc,
		URLs:            v3fmt.NewURLBuilder(baseURL),
		LocalReposRoot:  os.Getenv("LOCAL_REPOS_ROOT"),
	}
}
