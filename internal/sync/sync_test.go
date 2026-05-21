package sync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"gitbucket/internal/db"
)

func TestEphemeralCredentialCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := NewEphemeralCredentialCache(ctx, 100*time.Millisecond)

	key := "test-pat"
	secret := []byte("ghp_SecretPAT12345")

	// Set value
	cache.Set(key, secret, 150*time.Millisecond)

	// Retrieve value
	got, found := cache.Get(key)
	if !found {
		t.Fatal("expected key to be found in cache")
	}
	if string(got) != "ghp_SecretPAT12345" {
		t.Errorf("expected secret to match, got %s", string(got))
	}

	// Zero out returned copy and check that cached copy is unaffected
	got[0] = 'X'
	got2, _ := cache.Get(key)
	if string(got2) != "ghp_SecretPAT12345" {
		t.Error("mutating retrieved secret copy mutated cached secret")
	}

	// Wait for expiration
	time.Sleep(250 * time.Millisecond)
	_, found = cache.Get(key)
	if found {
		t.Error("expected secret to be expired and not found")
	}

	// Test zeroization check on eviction
	cache.CleanupExpired()
	cache.mu.RLock()
	entry, exists := cache.cache[key]
	cache.mu.RUnlock()
	if exists {
		// Verify zeroized
		for i, b := range entry.DecryptedSecret {
			if b != 0 {
				t.Errorf("byte at index %d was not zeroed: %d", i, b)
			}
		}
	}
}

func TestRunGitCommandProcessGroupKill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pr, pw := io.Pipe()
	go func() {
		<-ctx.Done()
		_ = pw.Close()
	}()
	defer pw.Close()
	defer pr.Close()

	// Use git hash-object with a blocking stdin pipe to make the command block until context timeout.
	err := RunGitCommand(ctx, []string{"hash-object", "--stdin-paths"}, ".", pr, nil, nil)
	if err == nil {
		t.Error("expected command to be killed and return error")
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestLeasedLocks(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping LeasedLocks tests: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer client.Close()

	owner := "lock-owner"
	repo := "lock-repo"
	worker1 := "worker-1"
	worker2 := "worker-2"

	// Clean up any stale lock
	_ = db.ReleaseLeasedLock(ctx, client, owner, repo, worker1)
	_ = db.ReleaseLeasedLock(ctx, client, owner, repo, worker2)

	// 1. Acquire lock with 5-second lease
	err = db.AcquireLeasedLock(ctx, client, owner, repo, worker1, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// 2. Try to acquire again by same/another worker (should fail)
	err = db.AcquireLeasedLock(ctx, client, owner, repo, worker2, 5*time.Second)
	if err == nil {
		t.Error("expected lock acquisition to fail due to active lease, but succeeded")
	}

	// 3. Test heartbeat context cancellation on failure
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	// Renewing by correct worker should succeed
	err = db.RenewLeasedLock(cancelCtx, client, owner, repo, worker1, 5*time.Second)
	if err != nil {
		t.Errorf("failed to renew lock: %v", err)
	}

	// Try heartbeat renewal with worker2 (should fail and call cancelFunc)
	heartbeatChan := db.StartLockHeartbeat(cancelCtx, cancelFunc, client, owner, repo, worker2, 50*time.Millisecond, 5*time.Second)

	select {
	case <-cancelCtx.Done():
		// Success: cancellation was triggered because renewal failed
	case <-time.After(500 * time.Millisecond):
		t.Error("expected heartbeat failure to trigger context cancellation")
	}
	close(heartbeatChan)

	// 4. Test Lock Stealing after expiry
	err = db.ReleaseLeasedLock(ctx, client, owner, repo, worker1)
	if err != nil {
		t.Errorf("failed to release lock: %v", err)
	}
	err = db.AcquireLeasedLock(ctx, client, owner, repo, worker1, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to acquire short lock: %v", err)
	}

	time.Sleep(800 * time.Millisecond) // wait for original 500ms lock lease to expire
	err = db.AcquireLeasedLock(ctx, client, owner, repo, worker2, 2*time.Second)
	if err != nil {
		t.Errorf("failed to steal expired lock: %v", err)
	}

	// 5. Release Lock
	err = db.ReleaseLeasedLock(ctx, client, owner, repo, worker2)
	if err != nil {
		t.Errorf("failed to release lock: %v", err)
	}
}

func TestExecuteSync(t *testing.T) {
	firestoreEmulator := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if firestoreEmulator == "" {
		t.Skip("Skipping TestExecuteSync: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()

	// 1. Setup mock GCS server and client
	mockGCS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/o") {
			w.Write([]byte(`{"items": []}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer mockGCS.Close()

	origEmulator := os.Getenv("STORAGE_EMULATOR_HOST")
	defer os.Setenv("STORAGE_EMULATOR_HOST", origEmulator)
	os.Setenv("STORAGE_EMULATOR_HOST", mockGCS.Listener.Addr().String())

	storageClient, err := storage.NewClient(ctx, option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("failed to create storage client: %v", err)
	}
	defer storageClient.Close()

	// 2. Setup Firestore client
	firestoreClient, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer firestoreClient.Close()

	// 3. Create temp directory for repos
	tempDir, err := os.MkdirTemp("", "gitbucket-sync-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	owner := "syncowner"
	repo := "syncrepo"
	localReposRoot := filepath.Join(tempDir, "local")
	mockGithubRoot := filepath.Join(tempDir, "github")

	gitbucketLocalPath := filepath.Join(localReposRoot, owner, repo+".git")
	githubMockPath := filepath.Join(mockGithubRoot, owner, repo+".git")

	// Helper to run git commands
	runGit := func(dir string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git command failed: git %s in %s: %v: %s", strings.Join(args, " "), dir, err, stderr.String())
		}
		return stdout.String()
	}

	// 4. Initialize GitHub Mock Repo with one commit
	githubWorkDir := filepath.Join(tempDir, "github_work")
	if err := os.MkdirAll(githubWorkDir, 0755); err != nil {
		t.Fatalf("failed to create github work dir: %v", err)
	}
	runGit(githubWorkDir, "init", "-b", "main")
	runGit(githubWorkDir, "config", "user.name", "GitHub User")
	runGit(githubWorkDir, "config", "user.email", "gh@example.com")
	if err := os.WriteFile(filepath.Join(githubWorkDir, "gh_file.txt"), []byte("github content"), 0644); err != nil {
		t.Fatalf("failed to write gh_file.txt: %v", err)
	}
	runGit(githubWorkDir, "add", ".")
	runGit(githubWorkDir, "commit", "-m", "GitHub commit")
	
	if err := os.MkdirAll(filepath.Dir(githubMockPath), 0755); err != nil {
		t.Fatalf("failed to create github mock dir: %v", err)
	}
	runGit(githubWorkDir, "clone", "--bare", githubWorkDir, githubMockPath)

	// 5. Initialize GitBucket Local Repo with a different commit
	gitbucketWorkDir := filepath.Join(tempDir, "gitbucket_work")
	if err := os.MkdirAll(gitbucketWorkDir, 0755); err != nil {
		t.Fatalf("failed to create gitbucket work dir: %v", err)
	}
	runGit(gitbucketWorkDir, "init", "-b", "main")
	runGit(gitbucketWorkDir, "config", "user.name", "GitBucket User")
	runGit(gitbucketWorkDir, "config", "user.email", "gb@example.com")
	if err := os.WriteFile(filepath.Join(gitbucketWorkDir, "gb_file.txt"), []byte("gitbucket content"), 0644); err != nil {
		t.Fatalf("failed to write gb_file.txt: %v", err)
	}
	runGit(gitbucketWorkDir, "add", ".")
	runGit(gitbucketWorkDir, "commit", "-m", "GitBucket commit")

	if err := os.MkdirAll(filepath.Dir(gitbucketLocalPath), 0755); err != nil {
		t.Fatalf("failed to create gitbucket local dir: %v", err)
	}
	runGit(gitbucketWorkDir, "clone", "--bare", gitbucketWorkDir, gitbucketLocalPath)

	// Configure git url rewriting so Git thinks githubMockPath is github.com
	runGit(gitbucketLocalPath, "config", "url."+githubMockPath+".insteadOf", "https://mocktoken@github.com/"+owner+"/"+repo+".git")

	// Clean up existing repository metadata to ensure idempotency
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := firestoreClient.Collection("repositories").Doc(repoId)
	_, _ = repoRef.Collection("secrets").Doc("github").Delete(ctx)
	_, _ = repoRef.Delete(ctx)
	// Clean up lock document just in case
	_, _ = firestoreClient.Collection("locks").Doc(repoId).Delete(ctx)

	// 6. Write Repository Metadata to Firestore
	err = db.CreateRepositoryMetadata(ctx, firestoreClient, "owner-uid", owner, repo, "Sync Test Repo", "public")
	if err != nil {
		t.Fatalf("failed to create repo metadata: %v", err)
	}

	metaMap := map[string]interface{}{
		"githubSync": map[string]interface{}{
			"enabled":    true,
			"githubRepo": owner + "/" + repo,
			"direction":  "mirror-to-github",
			"syncStatus": "idle",
		},
	}
	err = db.UpdateRepositoryMetadata(ctx, firestoreClient, owner, repo, metaMap)
	if err != nil {
		t.Fatalf("failed to update repo metadata: %v", err)
	}

	// 7. Encrypt and save mock token
	kmsClient := NewKMSClient(nil, "")
	encToken, encDEK, nonce, err := kmsClient.EncryptSecret(ctx, "mocktoken")
	if err != nil {
		t.Fatalf("failed to encrypt mock token: %v", err)
	}
	
	secretRef := firestoreClient.Collection("repositories").Doc(owner+"_"+repo).Collection("secrets").Doc("github")
	_, err = secretRef.Set(ctx, map[string]interface{}{
		"encryptedToken": encToken,
		"encryptedDEK":   encDEK,
		"nonce":          nonce,
		"authMethod":     "pat",
	})
	if err != nil {
		t.Fatalf("failed to save encrypted credential: %v", err)
	}

	// 8. Execute mirror-to-github (force = true)
	res, err := ExecuteSync(ctx, firestoreClient, storageClient, "test-bucket", localReposRoot, kmsClient, owner, repo, "mirror-to-github", true, "force-gitbucket")
	if err != nil {
		t.Fatalf("ExecuteSync failed: %v", err)
	}
	if res.Status != "synced" {
		t.Fatalf("unexpected sync status: %s (error: %s)", res.Status, res.ErrorMessage)
	}

	// 9. Verify that the GitHub mock repo has received the GitBucket commit
	logOutput := runGit(githubMockPath, "log", "--oneline")
	if !strings.Contains(logOutput, "GitBucket commit") {
		t.Errorf("expected GitHub mock repo to contain 'GitBucket commit', got log: %s", logOutput)
	}
}

