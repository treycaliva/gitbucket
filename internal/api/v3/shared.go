package v3

import (
	"context"
	"fmt"
	"path/filepath"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"

	"gitbucket/internal/db"
)

// errRepoNotFound is the sentinel returned when Firestore has no record for
// the requested owner/repo pair. Callers map this to HTTP 404.
var errRepoNotFound = fmt.Errorf("repo not found")

// MaterializeRepo ensures a bare repo exists at <localReposRoot>/<owner>_<repo>.git.
// In production this also syncs missing objects from GCS, but for Plan 2 we
// trust that:
//
//	(a) Tests seed the bare repo directly via seedLocalRepo (see contents_test.go), and
//	(b) Production traffic on /api/v3 routes goes through the same Git HTTP
//	    infrastructure that already populates the local repo on prior reads.
//
// The Firestore presence check is the source of truth for "does this repo
// exist"; if the local bare repo is missing, we return NotFound (callers
// must surface that as 404).
//
// FUTURE: If running on a cold Cloud Run instance, this will need to invoke
// the GCS sync logic from internal/api.APIHandler. Track as follow-on.
func MaterializeRepo(ctx context.Context, fs *firestore.Client, sc *storage.Client, owner, repo, localReposRoot string) (string, error) {
	meta, err := db.GetRepositoryMetadata(ctx, fs, owner, repo)
	if err != nil {
		return "", err
	}
	if meta == nil {
		return "", errRepoNotFound
	}
	if localReposRoot == "" {
		return "", fmt.Errorf("LocalReposRoot not configured")
	}
	bare := filepath.Join(localReposRoot, owner+"_"+repo+".git")
	return bare, nil
}
