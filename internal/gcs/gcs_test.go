package gcs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

func TestGCSRepositoryOperations(t *testing.T) {
	bucketName := os.Getenv("GCS_BUCKET")
	emulatorHost := os.Getenv("STORAGE_EMULATOR_HOST")
	credsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")

	// Skip if no GCS environment/emulator is available
	if bucketName == "" && emulatorHost == "" && credsFile == "" {
		t.Skip("Skipping GCS integration tests: no bucket, emulator host, or credentials defined")
	}

	if bucketName == "" {
		bucketName = "git-bucket-repositories-79382"
	}

	ctx := context.Background()

	var client *storage.Client
	var err error

	if emulatorHost != "" {
		// Connect to emulator
		client, err = storage.NewClient(ctx, option.WithoutAuthentication())
	} else {
		client, err = NewClient(ctx)
	}

	if err != nil {
		t.Fatalf("failed to create GCS client: %v", err)
	}
	defer client.Close()

	// Set up local repository root and cache
	localReposRoot, err := os.MkdirTemp("", "gcs-repos-root-*")
	if err != nil {
		t.Fatalf("failed to create local repos root: %v", err)
	}
	defer os.RemoveAll(localReposRoot)

	owner := "gcs-owner"
	repo := "gcs-repo"

	localRepoPath := filepath.Join(localReposRoot, owner, repo+".git")
	if err := os.MkdirAll(localRepoPath, 0755); err != nil {
		t.Fatalf("failed to create local repo dir: %v", err)
	}

	testFile := filepath.Join(localRepoPath, "HEAD")
	expectedContent := "ref: refs/heads/main\n"
	if err := os.WriteFile(testFile, []byte(expectedContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// 1. Upload repository
	err = UploadRepo(ctx, client, bucketName, owner, repo, localReposRoot)
	if err != nil {
		t.Fatalf("UploadRepo failed: %v", err)
	}

	// Remove local repository folder to test download
	if err := os.RemoveAll(localRepoPath); err != nil {
		t.Fatalf("failed to clean local repo before download: %v", err)
	}

	// 2. Download repository
	downloaded, err := DownloadRepo(ctx, client, bucketName, owner, repo, localReposRoot)
	if err != nil {
		t.Fatalf("DownloadRepo failed: %v", err)
	}
	if !downloaded {
		t.Fatal("expected DownloadRepo to return true, indicating the repository was found and downloaded")
	}

	// Verify downloaded content
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("downloaded content mismatch: expected %q, got %q", expectedContent, string(content))
	}

	// 3. Delete repository
	err = DeleteRepo(ctx, client, bucketName, owner, repo, localReposRoot)
	if err != nil {
		t.Fatalf("DeleteRepo failed: %v", err)
	}

	// Verify local folder is deleted
	if _, err := os.Stat(localRepoPath); !os.IsNotExist(err) {
		t.Error("expected local repository folder to be deleted")
	}

	// Verify it's deleted from GCS
	downloaded, err = DownloadRepo(ctx, client, bucketName, owner, repo, localReposRoot)
	if err != nil {
		t.Fatalf("DownloadRepo checking deletion failed: %v", err)
	}
	if downloaded {
		t.Error("expected DownloadRepo to return false for deleted repository")
	}
}
