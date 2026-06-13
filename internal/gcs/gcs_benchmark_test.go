package gcs

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

func createBenchmarkSetup(b *testing.B, numFiles int) (context.Context, *storage.Client, string, string, string, string) {
	b.Helper()

	bucketName := os.Getenv("GCS_BUCKET")
	emulatorHost := os.Getenv("STORAGE_EMULATOR_HOST")
	credsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")

	if bucketName == "" && emulatorHost == "" && credsFile == "" {
		b.Skip("Skipping GCS benchmark tests: no bucket, emulator host, or credentials defined")
	}

	if bucketName == "" {
		bucketName = "git-bucket-repositories-79382"
	}

	ctx := context.Background()

	var client *storage.Client
	var err error

	if emulatorHost != "" {
		client, err = storage.NewClient(ctx, option.WithoutAuthentication())
	} else {
		client, err = NewClient(ctx)
	}

	if err != nil {
		b.Fatalf("failed to create GCS client: %v", err)
	}

	localReposRoot, err := os.MkdirTemp("", "gcs-repos-root-bench-*")
	if err != nil {
		b.Fatalf("failed to create local repos root: %v", err)
	}

	owner := "benchmark-owner"
	repo := "benchmark-repo-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	localRepoPath := filepath.Join(localReposRoot, owner, repo+".git")
	if err := os.MkdirAll(localRepoPath, 0755); err != nil {
		b.Fatalf("failed to create local repo dir: %v", err)
	}

	// Create random files
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < numFiles; i++ {
		testFile := filepath.Join(localRepoPath, fmt.Sprintf("file_%d.txt", i))
		content := fmt.Sprintf("content_%d_%d", i, rand.Int())
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			b.Fatalf("failed to write test file: %v", err)
		}
	}

	return ctx, client, bucketName, owner, repo, localReposRoot
}

func BenchmarkUploadRepoDelete(b *testing.B) {
	numFiles := 20 // Adjust based on reasonable limit

	ctx, client, bucketName, owner, repo, localReposRoot := createBenchmarkSetup(b, numFiles)
	defer client.Close()
	defer os.RemoveAll(localReposRoot)

	// Setup: initial upload of all files
	err := UploadRepo(ctx, client, bucketName, owner, repo, localReposRoot)
	if err != nil {
		b.Fatalf("UploadRepo initial failed: %v", err)
	}

	// Now clear the local files so UploadRepo will have to delete them from GCS
	localRepoPath := filepath.Join(localReposRoot, owner, repo+".git")
	files, err := os.ReadDir(localRepoPath)
	if err != nil {
		b.Fatalf("failed to read local dir: %v", err)
	}
	for _, f := range files {
		os.Remove(filepath.Join(localRepoPath, f.Name()))
	}

	b.ResetTimer()

	// This is the actual benchmark
	// Note: UploadRepo does more than just delete, but the deletion is the part we're benchmarking and it will dominate the time since local is empty.
	for i := 0; i < b.N; i++ {
		// Pause timer to re-setup state
		b.StopTimer()

		// Re-upload files
		// We bypass UploadRepo for setup because it would delete again

		// Setup local files again
		for j := 0; j < numFiles; j++ {
			testFile := filepath.Join(localRepoPath, fmt.Sprintf("file_%d.txt", j))
			content := fmt.Sprintf("content_%d_%d", j, rand.Int())
			if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
				b.Fatalf("failed to write test file: %v", err)
			}
		}

		// Fast upload to setup GCS state for deletion
		err = UploadRepo(ctx, client, bucketName, owner, repo, localReposRoot)
		if err != nil {
			b.Fatalf("UploadRepo initial failed: %v", err)
		}

		// Remove local files to trigger deletion during next UploadRepo
		for j := 0; j < numFiles; j++ {
			testFile := filepath.Join(localRepoPath, fmt.Sprintf("file_%d.txt", j))
			os.Remove(testFile)
		}

		b.StartTimer()

		err = UploadRepo(ctx, client, bucketName, owner, repo, localReposRoot)
		if err != nil {
			b.Fatalf("UploadRepo delete phase failed: %v", err)
		}
	}
}
