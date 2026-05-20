package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestGetUsernameByUID(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping firestore db test: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	suffix := randSuffix()
	uid := "test-uid-" + suffix
	expectedUsername := "test-user-" + suffix

	// Insert test user directly into firestore
	_, err = client.Collection("users").Doc(uid).Set(ctx, map[string]interface{}{
		"username": expectedUsername,
	})
	if err != nil {
		t.Fatalf("failed to set test user document: %v", err)
	}

	username, err := GetUsernameByUID(ctx, client, uid)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if username != expectedUsername {
		t.Errorf("expected username %s, got %s", expectedUsername, username)
	}
}

func TestUsernameRegistryFlow(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping firestore db test: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	suffix := randSuffix()
	uid1 := "uid-reg-1-" + suffix
	username1 := "user-reg-1-" + suffix
	email1 := "user1-" + suffix + "@example.com"

	uid2 := "uid-reg-2-" + suffix
	email2 := "user2-" + suffix + "@example.com"

	// Register first user
	err = RegisterUsername(ctx, client, uid1, username1, email1)
	if err != nil {
		t.Fatalf("failed to register user 1: %v", err)
	}

	// Verify lookups
	uidFetched, err := GetUidByUsername(ctx, client, username1)
	if err != nil {
		t.Fatalf("failed to get uid by username: %v", err)
	}
	if uidFetched != uid1 {
		t.Errorf("expected uid %s, got %s", uid1, uidFetched)
	}

	usernameFetched, err := GetUsernameByUID(ctx, client, uid1)
	if err != nil {
		t.Fatalf("failed to get username by uid: %v", err)
	}
	if usernameFetched != username1 {
		t.Errorf("expected username %s, got %s", username1, usernameFetched)
	}

	// Try to register duplicate username with different uid (should fail)
	err = RegisterUsername(ctx, client, uid2, username1, email2)
	if err == nil {
		t.Error("expected registration of duplicate username to fail")
	}

	// Try to register new username for already registered uid1 (should fail)
	err = RegisterUsername(ctx, client, uid1, "new-username-"+suffix, email1)
	if err == nil {
		t.Error("expected registration of new username for already registered uid to fail")
	}
}

func TestPATLifecycle(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping firestore db test: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	suffix := randSuffix()
	uid := "pat-owner-uid-" + suffix
	username := "pat-owner-user-" + suffix
	email := "pat-owner-" + suffix + "@example.com"

	// Register user first because VerifyPAT fetches username
	err = RegisterUsername(ctx, client, uid, username, email)
	if err != nil {
		t.Fatalf("failed to register user for PAT testing: %v", err)
	}

	// Generate PAT
	tokenName := "my-api-token-" + suffix
	rawToken, err := GeneratePAT(ctx, client, uid, tokenName)
	if err != nil {
		t.Fatalf("failed to generate PAT: %v", err)
	}

	if !strings.HasPrefix(rawToken, "gb_pat_") {
		t.Errorf("expected token prefix 'gb_pat_', got %s", rawToken)
	}

	// Verify PAT
	verifiedUID, verifiedUsername, err := VerifyPAT(ctx, client, rawToken)
	if err != nil {
		t.Fatalf("failed to verify PAT: %v", err)
	}
	if verifiedUID != uid {
		t.Errorf("expected verified uid %s, got %s", uid, verifiedUID)
	}
	if verifiedUsername != username {
		t.Errorf("expected verified username %s, got %s", username, verifiedUsername)
	}

	// List PATs
	pats, err := ListPATs(ctx, client, uid)
	if err != nil {
		t.Fatalf("failed to list PATs: %v", err)
	}
	if len(pats) != 1 {
		t.Fatalf("expected 1 PAT, got %d", len(pats))
	}
	if pats[0].Name != tokenName {
		t.Errorf("expected token name %s, got %s", tokenName, pats[0].Name)
	}
	tokenId := pats[0].ID
	if tokenId == "" {
		t.Error("expected non-empty token ID")
	}

	// Revoke PAT by other user should fail
	err = RevokePAT(ctx, client, "other-uid-"+suffix, tokenId)
	if err == nil {
		t.Error("expected revoking other user's token to fail")
	}

	// Revoke PAT by owner
	err = RevokePAT(ctx, client, uid, tokenId)
	if err != nil {
		t.Fatalf("failed to revoke PAT: %v", err)
	}

	// Verify it's revoked
	_, _, err = VerifyPAT(ctx, client, rawToken)
	if err == nil {
		t.Error("expected verified revoked PAT to return error")
	}

	// List PATs again
	pats, err = ListPATs(ctx, client, uid)
	if err != nil {
		t.Fatalf("failed to list PATs: %v", err)
	}
	if len(pats) != 0 {
		t.Errorf("expected 0 PATs after revocation, got %d", len(pats))
	}
}

func TestDistributedLocks(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping firestore db lock test: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	suffix := randSuffix()
	owner := "owner-" + suffix
	repo := "repo-" + suffix

	// 1. Acquire lock first time
	token, err := AcquireLock(ctx, client, owner, repo, 2*time.Second, 1*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// 2. Try to acquire again (should fail/timeout)
	_, err = AcquireLock(ctx, client, owner, repo, 2*time.Second, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected failure to acquire already locked repository")
	}

	// 3. Release lock with wrong token (should not delete)
	err = ReleaseLock(ctx, client, owner, repo, "wrong-token")
	if err != nil {
		t.Fatalf("ReleaseLock with wrong token returned unexpected error: %v", err)
	}

	// Lock should still be held, so acquiring with new token should fail
	_, err = AcquireLock(ctx, client, owner, repo, 2*time.Second, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected lock to still be held after wrong token release")
	}

	// 4. Release lock with correct token
	err = ReleaseLock(ctx, client, owner, repo, token)
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}

	// 5. Acquire lock again (should succeed immediately)
	token2, err := AcquireLock(ctx, client, owner, repo, 2*time.Second, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to re-acquire lock after release: %v", err)
	}
	if token2 == "" {
		t.Fatal("expected non-empty token2")
	}

	// 6. Test lock expiration: set lease duration to 100ms, timeout 500ms
	ownerExp := "owner-exp-" + suffix
	repoExp := "repo-exp-" + suffix
	tokenExp1, err := AcquireLock(ctx, client, ownerExp, repoExp, 100*time.Millisecond, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to acquire first expiration lock: %v", err)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be able to acquire again even without releasing
	tokenExp2, err := AcquireLock(ctx, client, ownerExp, repoExp, 2*time.Second, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to acquire expired lock: %v", err)
	}
	if tokenExp2 == "" || tokenExp2 == tokenExp1 {
		t.Fatalf("expected new token, got tokenExp1=%s, tokenExp2=%s", tokenExp1, tokenExp2)
	}
}

func TestRepositoryMetadataLifecycle(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping firestore db repo metadata test: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	suffix := randSuffix()
	ownerUid := "user-uid-" + suffix
	ownerUsername := "User-Name-" + suffix // test case sensitivity handling
	repoName := "Repo-Name-" + suffix      // test case sensitivity handling
	description := "A cool test repo description"
	visibility := "public"

	// 1. Create Repository Metadata
	err = CreateRepositoryMetadata(ctx, client, ownerUid, ownerUsername, repoName, description, visibility)
	if err != nil {
		t.Fatalf("failed to create repository metadata: %v", err)
	}

	// 2. Try to create duplicate (should fail)
	err = CreateRepositoryMetadata(ctx, client, ownerUid, ownerUsername, repoName, description, visibility)
	if err == nil {
		t.Fatal("expected duplicate repository creation to fail")
	}

	// 3. Get repository metadata
	meta, err := GetRepositoryMetadata(ctx, client, ownerUsername, repoName)
	if err != nil {
		t.Fatalf("failed to get repository metadata: %v", err)
	}
	if meta == nil {
		t.Fatal("expected repository metadata to be found")
	}

	// Check fields
	if meta["ownerUid"] != ownerUid {
		t.Errorf("expected ownerUid %s, got %v", ownerUid, meta["ownerUid"])
	}
	if meta["owner"] != strings.ToLower(ownerUsername) {
		t.Errorf("expected owner %s, got %v", strings.ToLower(ownerUsername), meta["owner"])
	}
	if meta["name"] != repoName {
		t.Errorf("expected name %s, got %v", repoName, meta["name"])
	}
	if meta["description"] != description {
		t.Errorf("expected description %s, got %v", description, meta["description"])
	}
	if meta["visibility"] != visibility {
		t.Errorf("expected visibility %s, got %v", visibility, meta["visibility"])
	}
	if meta["defaultBranch"] != "main" {
		t.Errorf("expected defaultBranch main, got %v", meta["defaultBranch"])
	}

	// 4. Update Repository Metadata
	newDesc := "Updated description"
	err = UpdateRepositoryMetadata(ctx, client, ownerUsername, repoName, map[string]interface{}{
		"description": newDesc,
		"visibility":  "private",
	})
	if err != nil {
		t.Fatalf("failed to update repository metadata: %v", err)
	}

	// Verify update
	metaUpdated, err := GetRepositoryMetadata(ctx, client, ownerUsername, repoName)
	if err != nil {
		t.Fatalf("failed to get updated repository metadata: %v", err)
	}
	if metaUpdated["description"] != newDesc {
		t.Errorf("expected updated description %s, got %v", newDesc, metaUpdated["description"])
	}
	if metaUpdated["visibility"] != "private" {
		t.Errorf("expected updated visibility private, got %v", metaUpdated["visibility"])
	}

	// 5. List user repositories
	repos, err := ListUserRepositories(ctx, client, ownerUsername)
	if err != nil {
		t.Fatalf("failed to list user repositories: %v", err)
	}
	found := false
	for _, r := range repos {
		if r["name"] == repoName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected repository %s in user's repository list", repoName)
	}

	// 6. List all public repositories
	publicRepos, err := ListAllPublicRepositories(ctx, client)
	if err != nil {
		t.Fatalf("failed to list all public repositories: %v", err)
	}
	for _, r := range publicRepos {
		if r["name"] == repoName && r["owner"] == strings.ToLower(ownerUsername) {
			t.Errorf("expected repository %s/%s not to be in public list after setting to private", ownerUsername, repoName)
		}
	}

	// Change visibility back to public to test ListAllPublicRepositories inclusion
	err = UpdateRepositoryMetadata(ctx, client, ownerUsername, repoName, map[string]interface{}{
		"visibility": "public",
	})
	if err != nil {
		t.Fatalf("failed to update visibility back to public: %v", err)
	}

	publicRepos, err = ListAllPublicRepositories(ctx, client)
	if err != nil {
		t.Fatalf("failed to list all public repositories: %v", err)
	}
	foundPublic := false
	for _, r := range publicRepos {
		if r["name"] == repoName && r["owner"] == strings.ToLower(ownerUsername) {
			foundPublic = true
			break
		}
	}
	if !foundPublic {
		t.Errorf("expected repository %s/%s to be in public list", ownerUsername, repoName)
	}

	// 7. Delete repository metadata
	err = DeleteRepositoryMetadata(ctx, client, ownerUsername, repoName)
	if err != nil {
		t.Fatalf("failed to delete repository metadata: %v", err)
	}

	// Verify deletion
	metaDeleted, err := GetRepositoryMetadata(ctx, client, ownerUsername, repoName)
	if err != nil {
		t.Fatalf("failed to get deleted repository metadata: %v", err)
	}
	if metaDeleted != nil {
		t.Fatal("expected repository metadata to be deleted")
	}
}
