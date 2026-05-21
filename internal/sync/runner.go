package sync

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"

	"gitbucket/internal/db"
	"gitbucket/internal/gcs"
)

// SyncResult holds the status details after a sync execution.
type SyncResult struct {
	Status       string
	ErrorMessage string
	GitbucketSha string
	GithubSha    string
}

// ExecuteSync performs the actual Git synchronization between GitBucket and GitHub.
func ExecuteSync(ctx context.Context, firestoreClient *firestore.Client, storageClient *storage.Client, bucketName, localReposRoot string, kmsClient *KMSClient, owner, repo, direction string, force bool, forceStrategy string) (*SyncResult, error) {
	log.Printf("[Sync] Initializing sync job for %s/%s (direction=%s, force=%v, strategy=%s)", owner, repo, direction, force, forceStrategy)

	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := firestoreClient.Collection("repositories").Doc(repoId)

	// 1. Fetch the encrypted GitHub credentials from Firestore
	secretDoc, err := repoRef.Collection("secrets").Doc("github").Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub credentials: %w", err)
	}

	var secretData struct {
		EncryptedToken string `firestore:"encryptedToken"`
		EncryptedDEK   string `firestore:"encryptedDEK"`
		Nonce          string `firestore:"nonce"`
	}
	if err := secretDoc.DataTo(&secretData); err != nil {
		return nil, fmt.Errorf("failed to parse secret data: %w", err)
	}

	if secretData.EncryptedToken == "" {
		return nil, fmt.Errorf("GitHub credential token is missing or empty")
	}

	// 2. Decrypt the secret token
	decryptedToken, err := kmsClient.DecryptSecretWithCache(ctx, secretData.EncryptedToken, secretData.EncryptedDEK, secretData.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt GitHub credentials: %w", err)
	}
	defer func() {
		// Zero out decrypted credentials in memory immediately after use
		for i := range decryptedToken {
			decryptedToken[i] = 0
		}
	}()

	// 3. Acquire the leased lock in Firestore
	workerID := fmt.Sprintf("worker-%d", time.Now().UnixNano())
	err = db.AcquireLeasedLock(ctx, firestoreClient, owner, repo, workerID, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire leased lock: %w", err)
	}
	defer func() {
		if err := db.ReleaseLeasedLock(context.Background(), firestoreClient, owner, repo, workerID); err != nil {
			log.Printf("[Sync Lock] Warning: failed to release lock: %v", err)
		}
	}()

	// 4. Start the lock heartbeat renewal routine
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	stopHeartbeat := db.StartLockHeartbeat(heartbeatCtx, cancelHeartbeat, firestoreClient, owner, repo, workerID, 30*time.Second, 5*time.Minute)
	defer close(stopHeartbeat)

	// 5. Download repository from GCS to local bare repo directory
	localRepoPath := filepath.Join(localReposRoot, strings.ToLower(owner), fmt.Sprintf("%s.git", strings.ToLower(repo)))
	foundAny, err := gcs.DownloadRepo(heartbeatCtx, storageClient, bucketName, owner, repo, localReposRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to download repo from GCS: %w", err)
	}

	// If no repository exists in GCS, check if local directory exists. If not, initialize it.
	if !foundAny {
		if _, err := os.Stat(localRepoPath); os.IsNotExist(err) {
			if err := os.MkdirAll(localRepoPath, 0755); err != nil {
				return nil, fmt.Errorf("failed to create local repo dir: %w", err)
			}
			initCmd := exec.CommandContext(heartbeatCtx, "git", "init", "--bare", localRepoPath)
			if err := initCmd.Run(); err != nil {
				return nil, fmt.Errorf("failed to initialize bare git repository: %w", err)
			}
		}
	}

	// 6. Fetch metadata to retrieve the target GitHub repository name
	metaMap, err := db.GetRepositoryMetadata(heartbeatCtx, firestoreClient, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository metadata: %w", err)
	}
	meta := db.MapToRepositoryMetadata(metaMap)
	if meta.GitHubSync.GitHubRepo == "" {
		return nil, fmt.Errorf("GitHub repository sync target is not configured")
	}

	// 7. Format GitHub remote URL with credentials
	githubRepoUrl := fmt.Sprintf("https://%s@github.com/%s.git", string(decryptedToken), meta.GitHubSync.GitHubRepo)

	// 8. Fetch from GitHub remote into refs/remotes/github/* namespace
	fetchArgs := []string{"--git-dir=" + localRepoPath, "fetch", githubRepoUrl, "+refs/heads/*:refs/remotes/github/*", "+refs/tags/*:refs/tags/*"}
	if err := RunGitCommand(heartbeatCtx, fetchArgs, "", nil, nil, nil); err != nil {
		return nil, fmt.Errorf("failed to fetch from GitHub: %w", err)
	}

	// 9. List local and GitHub references to construct updates
	var stdout bytes.Buffer
	listArgs := []string{"--git-dir=" + localRepoPath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/", "refs/remotes/github/"}
	if err := RunGitCommand(heartbeatCtx, listArgs, "", nil, &stdout, nil); err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}

	localRefs := make(map[string]string)
	githubRefs := make(map[string]string)
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			continue
		}
		refname := parts[0]
		sha := parts[1]
		if strings.HasPrefix(refname, "refs/heads/") {
			branch := strings.TrimPrefix(refname, "refs/heads/")
			localRefs[branch] = sha
		} else if strings.HasPrefix(refname, "refs/remotes/github/") {
			branch := strings.TrimPrefix(refname, "refs/remotes/github/")
			githubRefs[branch] = sha
		}
	}

	var pushRefspecs []string
	var localUpdates []struct{ branch, sha string }
	var conflictBranches []string

	// Helper to check ancestry
	isAncestor := func(ancestor, descendant string) (bool, error) {
		if ancestor == descendant {
			return true, nil
		}
		args := []string{"--git-dir=" + localRepoPath, "merge-base", "--is-ancestor", ancestor, descendant}
		err := RunGitCommand(heartbeatCtx, args, "", nil, nil, nil)
		if err == nil {
			return true, nil
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		return false, err
	}

	// Process differences depending on sync direction
	if direction == "mirror-to-github" {
		for branch, localSha := range localRefs {
			githubSha, exists := githubRefs[branch]
			if !exists {
				pushRefspecs = append(pushRefspecs, fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
				continue
			}

			ancestor, err := isAncestor(githubSha, localSha)
			if err != nil {
				return nil, err
			}
			if ancestor {
				if githubSha != localSha {
					pushRefspecs = append(pushRefspecs, fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
				}
				continue
			}

			if force && forceStrategy == "force-gitbucket" {
				pushRefspecs = append(pushRefspecs, fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, branch))
			} else {
				conflictBranches = append(conflictBranches, branch)
			}
		}

		// Prune remote branches that no longer exist locally in GitBucket
		defBranch := meta.DefaultBranch
		if defBranch == "" {
			defBranch = "main"
		}
		for branch := range githubRefs {
			if _, exists := localRefs[branch]; !exists {
				if branch != defBranch && branch != "" {
					log.Printf("[Sync Prune] Queueing deletion for GitHub branch: %s", branch)
					pushRefspecs = append(pushRefspecs, fmt.Sprintf(":refs/heads/%s", branch))
				}
			}
		}
	} else if direction == "mirror-to-gitbucket" {
		for branch, githubSha := range githubRefs {
			localSha, exists := localRefs[branch]
			if !exists {
				localUpdates = append(localUpdates, struct{ branch, sha string }{branch, githubSha})
				continue
			}

			ancestor, err := isAncestor(localSha, githubSha)
			if err != nil {
				return nil, err
			}
			if ancestor {
				if localSha != githubSha {
					localUpdates = append(localUpdates, struct{ branch, sha string }{branch, githubSha})
				}
				continue
			}

			if force && forceStrategy == "force-github" {
				localUpdates = append(localUpdates, struct{ branch, sha string }{branch, githubSha})
			} else {
				conflictBranches = append(conflictBranches, branch)
			}
		}
	} else if direction == "bidirectional" {
		allBranches := make(map[string]bool)
		for b := range localRefs {
			allBranches[b] = true
		}
		for b := range githubRefs {
			allBranches[b] = true
		}

		for branch := range allBranches {
			localSha, localExists := localRefs[branch]
			githubSha, githubExists := githubRefs[branch]

			if localExists && !githubExists {
				pushRefspecs = append(pushRefspecs, fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
			} else if !localExists && githubExists {
				localUpdates = append(localUpdates, struct{ branch, sha string }{branch, githubSha})
			} else {
				if localSha == githubSha {
					continue
				}

				gitbucketAhead, err := isAncestor(githubSha, localSha)
				if err != nil {
					return nil, err
				}
				if gitbucketAhead {
					pushRefspecs = append(pushRefspecs, fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
					continue
				}

				githubAhead, err := isAncestor(localSha, githubSha)
				if err != nil {
					return nil, err
				}
				if githubAhead {
					localUpdates = append(localUpdates, struct{ branch, sha string }{branch, githubSha})
					continue
				}

				if force {
					if forceStrategy == "force-gitbucket" {
						pushRefspecs = append(pushRefspecs, fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, branch))
					} else if forceStrategy == "force-github" {
						localUpdates = append(localUpdates, struct{ branch, sha string }{branch, githubSha})
					} else {
						conflictBranches = append(conflictBranches, branch)
					}
				} else {
					conflictBranches = append(conflictBranches, branch)
				}
			}
		}
	}

	if len(conflictBranches) > 0 {
		log.Printf("[Sync Conflict] Divergence detected on branches: %s", strings.Join(conflictBranches, ", "))
		return &SyncResult{
			Status:       "conflict",
			ErrorMessage: fmt.Sprintf("Sync conflict detected on branches: %s", strings.Join(conflictBranches, ", ")),
		}, nil
	}

	// 10. Execute local updates
	if len(localUpdates) > 0 {
		for _, update := range localUpdates {
			log.Printf("[Sync Update] Fast-forwarding local branch %s to %s", update.branch, update.sha)
			updateArgs := []string{"--git-dir=" + localRepoPath, "update-ref", fmt.Sprintf("refs/heads/%s", update.branch), update.sha}
			if err := RunGitCommand(heartbeatCtx, updateArgs, "", nil, nil, nil); err != nil {
				return nil, fmt.Errorf("failed to update local ref %s: %w", update.branch, err)
			}
		}
	}

	// 11. Execute push to GitHub remote
	if len(pushRefspecs) > 0 {
		log.Printf("[Sync Push] Pushing refspecs to GitHub: %v", pushRefspecs)
		pushArgs := append([]string{"--git-dir=" + localRepoPath, "push", githubRepoUrl}, pushRefspecs...)
		if err := RunGitCommand(heartbeatCtx, pushArgs, "", nil, nil, nil); err != nil {
			return nil, fmt.Errorf("failed to push to GitHub remote: %w", err)
		}

		// Record pushed SHAs for webhook loop prevention
		for _, refspec := range pushRefspecs {
			parts := strings.Split(refspec, ":")
			localRef := strings.TrimPrefix(parts[0], "+")
			branchName := strings.TrimPrefix(localRef, "refs/heads/")
			if sha, ok := localRefs[branchName]; ok {
				_ = recordSyncPushSHA(ctx, firestoreClient, sha)
			}
		}
	}

	// 12. Upload repo back to GCS if local changes were made
	if len(localUpdates) > 0 && storageClient != nil && bucketName != "" {
		log.Printf("[Sync Upload] Uploading updated repository cache back to GCS...")
		if err := gcs.UploadRepo(heartbeatCtx, storageClient, bucketName, owner, repo, localReposRoot); err != nil {
			return nil, fmt.Errorf("failed to upload repo to GCS: %w", err)
		}
	}

	// Get latest SHAs to return
	var currentGitbucketSha string
	var currentGithubSha string

	// Parse default branch or main branch SHA
	defaultBranch := meta.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	if sha, ok := localRefs[defaultBranch]; ok {
		currentGitbucketSha = sha
	}
	if sha, ok := githubRefs[defaultBranch]; ok {
		currentGithubSha = sha
	}

	// If default branch update was applied
	for _, u := range localUpdates {
		if u.branch == defaultBranch {
			currentGitbucketSha = u.sha
		}
	}

	log.Printf("[Sync Success] Repository sync completed successfully!")
	return &SyncResult{
		Status:       "synced",
		GitbucketSha: currentGitbucketSha,
		GithubSha:    currentGithubSha,
	}, nil
}

func recordSyncPushSHA(ctx context.Context, client *firestore.Client, sha string) error {
	if sha == "" || sha == "0000000000000000000000000000000000000000" {
		return nil
	}
	_, err := client.Collection("recent_sync_pushes").Doc(sha).Set(ctx, map[string]interface{}{
		"createdAt": firestore.ServerTimestamp,
	})
	return err
}
