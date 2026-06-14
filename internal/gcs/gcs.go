package gcs

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
)

// NewClient initializes the Google Cloud Storage client.
func NewClient(ctx context.Context) (*storage.Client, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}
	return client, nil
}

// DownloadRepo downloads the repository files from GCS, comparing with local cache.
// Returns true if downloaded successfully/exists, or false if the repository does not exist in GCS yet.
func DownloadRepo(ctx context.Context, client *storage.Client, bucketName, owner, repo, localReposRoot string) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("storage client is nil")
	}

	ownerLower := strings.ToLower(owner)
	repoLower := strings.ToLower(repo)
	prefix := fmt.Sprintf("repos/%s/%s/", ownerLower, repoLower)
	localRepoPath := filepath.Join(localReposRoot, ownerLower, fmt.Sprintf("%s.git", repoLower))

	bucket := client.Bucket(bucketName)
	it := bucket.Objects(ctx, &storage.Query{Prefix: prefix})

	var foundAny bool
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return false, fmt.Errorf("failed to list GCS objects: %w", err)
		}
		foundAny = true

		relPath := strings.TrimPrefix(attrs.Name, prefix)
		if relPath == "" {
			continue
		}
		if strings.HasPrefix(relPath, "lfs/") {
			continue
		}

		localPath := filepath.Join(localRepoPath, relPath)

		// Directory placeholder
		if strings.HasSuffix(attrs.Name, "/") {
			if err := os.MkdirAll(localPath, 0755); err != nil {
				return false, fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		localInfo, err := os.Stat(localPath)
		needsDownload := false
		if os.IsNotExist(err) {
			needsDownload = true
		} else if err != nil {
			return false, fmt.Errorf("failed to stat local file: %w", err)
		} else {
			// Git objects are immutable, so we only need to compare their size.
			// Other config/ref files might be overwritten, so we check mod time as well.
			isObject := strings.Contains(attrs.Name, "/objects/")
			if isObject {
				if localInfo.Size() != attrs.Size {
					needsDownload = true
				}
			} else {
				if localInfo.Size() != attrs.Size || localInfo.ModTime().Before(attrs.Updated) {
					needsDownload = true
				}
			}
		}

		if needsDownload {
			if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
				return false, fmt.Errorf("failed to create directory: %w", err)
			}

			// Remove the existing file if it exists, to avoid permission denied on read-only git files
			_ = os.Remove(localPath)

			rc, err := bucket.Object(attrs.Name).NewReader(ctx)
			if err != nil {
				return false, fmt.Errorf("failed to open GCS reader: %w", err)
			}

			f, err := os.OpenFile(localPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				rc.Close()
				return false, fmt.Errorf("failed to create local file: %w", err)
			}

			_, copyErr := io.Copy(f, rc)
			f.Close()
			rc.Close()
			if copyErr != nil {
				return false, fmt.Errorf("failed to write local file: %w", copyErr)
			}

			// Sync modified time
			_ = os.Chtimes(localPath, attrs.Updated, attrs.Updated)
		}
	}

	return foundAny, nil
}

// UploadRepo walks the local bare repository and uploads new/modified files to GCS.
// It also deletes files from GCS that are no longer present locally.
func UploadRepo(ctx context.Context, client *storage.Client, bucketName, owner, repo, localReposRoot string) error {
	if client == nil {
		return fmt.Errorf("storage client is nil")
	}

	ownerLower := strings.ToLower(owner)
	repoLower := strings.ToLower(repo)
	prefix := fmt.Sprintf("repos/%s/%s/", ownerLower, repoLower)
	localRepoPath := filepath.Join(localReposRoot, ownerLower, fmt.Sprintf("%s.git", repoLower))

	// Check if local repository path exists
	if _, err := os.Stat(localRepoPath); os.IsNotExist(err) {
		return fmt.Errorf("local repository directory does not exist: %s", localRepoPath)
	}

	// 1. Walk local files
	localFiles := make(map[string]os.FileInfo)
	err := filepath.Walk(localRepoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(localRepoPath, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "lfs/") {
			return nil
		}
		localFiles[rel] = info
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk local repository: %w", err)
	}

	// 2. Fetch GCS objects
	gcsFiles := make(map[string]*storage.ObjectAttrs)
	bucket := client.Bucket(bucketName)
	it := bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to list GCS objects: %w", err)
		}
		rel := strings.TrimPrefix(attrs.Name, prefix)
		if rel == "" || strings.HasSuffix(attrs.Name, "/") {
			continue
		}
		if strings.HasPrefix(rel, "lfs/") {
			continue
		}
		gcsFiles[rel] = attrs
	}

	// 3. Upload new/modified files
	for rel, info := range localFiles {
		gcsKey := prefix + rel
		needsUpload := false

		gcsAttr, exists := gcsFiles[rel]
		if !exists {
			needsUpload = true
		} else {
			if info.Size() != gcsAttr.Size || info.ModTime().After(gcsAttr.Updated) {
				needsUpload = true
			}
		}

		if needsUpload {
			f, err := os.Open(filepath.Join(localRepoPath, rel))
			if err != nil {
				return fmt.Errorf("failed to open local file: %w", err)
			}

			obj := bucket.Object(gcsKey)
			w := obj.NewWriter(ctx)
			w.CacheControl = "no-cache, no-store, must-revalidate"

			_, copyErr := io.Copy(w, f)
			f.Close()
			if copyErr != nil {
				w.Close()
				return fmt.Errorf("failed to copy file to GCS: %w", copyErr)
			}
			if err := w.Close(); err != nil {
				return fmt.Errorf("failed to close GCS writer: %w", err)
			}
		}
	}

	// 4. Delete GCS files that are no longer present locally
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(20) // Limit concurrency to avoid rate limits

	for rel, attrs := range gcsFiles {
		if _, exists := localFiles[rel]; !exists {
			name := attrs.Name // capture for goroutine
			eg.Go(func() error {
				log.Printf("[UploadRepo Delete] Deleting object in GCS: %s", name)
				obj := bucket.Object(name)
				if err := obj.Delete(egCtx); err != nil && err != storage.ErrObjectNotExist {
					return fmt.Errorf("failed to delete GCS object %s: %w", name, err)
				}
				return nil
			})
		}
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}

// DeleteRepo removes all repository files from GCS and deletes the local cache folder.
func DeleteRepo(ctx context.Context, client *storage.Client, bucketName, owner, repo, localReposRoot string) error {
	if client == nil {
		return fmt.Errorf("storage client is nil")
	}

	ownerLower := strings.ToLower(owner)
	repoLower := strings.ToLower(repo)
	prefix := fmt.Sprintf("repos/%s/%s/", ownerLower, repoLower)
	localRepoPath := filepath.Join(localReposRoot, ownerLower, fmt.Sprintf("%s.git", repoLower))

	// Delete all objects under the GCS prefix
	bucket := client.Bucket(bucketName)
	it := bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to list GCS objects for deletion: %w", err)
		}
		obj := bucket.Object(attrs.Name)
		if err := obj.Delete(ctx); err != nil && err != storage.ErrObjectNotExist {
			return fmt.Errorf("failed to delete GCS object %s: %w", attrs.Name, err)
		}
	}

	// Delete local cache folder
	if _, err := os.Stat(localRepoPath); err == nil {
		if err := os.RemoveAll(localRepoPath); err != nil {
			return fmt.Errorf("failed to delete local repository cache: %w", err)
		}
	}

	return nil
}
