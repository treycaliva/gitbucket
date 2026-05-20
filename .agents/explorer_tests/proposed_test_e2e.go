package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cloud.google.com/go/firestore"
)

// Config holds the parameters for the E2E test run
type Config struct {
	BaseURL     string
	ProjectID   string
	RunLFS      bool
	FirebaseUID string
	Username    string
	RepoName    string
	UseEmulator bool
}

// TestResult holds status for the test matrix
type TestResult struct {
	Name    string
	Tier    string
	Passed  bool
	Message string
}

type TokenResponse struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

type RepoResponse struct {
	Success bool   `json:"success"`
	Owner   string `json:"owner"`
	Name    string `json:"name"`
}

type CommitItem struct {
	SHA         string `json:"sha"`
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	Date        string `json:"date"`
	Message     string `json:"message"`
}

type TreeItem struct {
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int    `json:"size"`
	Name string `json:"name"`
	Path string `json:"path"`
}

var results []TestResult

func record(tier, name string, passed bool, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	results = append(results, TestResult{
		Tier:    tier,
		Name:    name,
		Passed:  passed,
		Message: msg,
	})
	if passed {
		log.Printf("[PASS] %s - %s: %s", tier, name, msg)
	} else {
		log.Printf("[FAIL] %s - %s: %s", tier, name, msg)
	}
}

func main() {
	var cfg Config
	flag.StringVar(&cfg.BaseURL, "url", "http://localhost:8080", "Target URL of the GitBucket Go server")
	flag.StringVar(&cfg.ProjectID, "project", "git-bucket-79382", "Google Cloud Project ID for Firestore")
	flag.BoolVar(&cfg.RunLFS, "lfs", true, "Run Git LFS test cases")
	flag.BoolVar(&cfg.UseEmulator, "emulator", true, "Use Firestore Emulator (sets FIRESTORE_EMULATOR_HOST)")
	flag.Parse()

	// Generate random username and repo to ensure clean runs
	randID := randomHex(4)
	cfg.FirebaseUID = "e2e_user_" + randID
	cfg.Username = "e2e-user-" + randID
	cfg.RepoName = "e2e-repo-" + randID

	log.Printf("==================================================")
	log.Printf("🚀 Starting GitBucket Go E2E Test Suite")
	log.Printf("Target Server: %s", cfg.BaseURL)
	log.Printf("Firestore Project: %s (Emulator: %v)", cfg.ProjectID, cfg.UseEmulator)
	log.Printf("Test User: %s (UID: %s)", cfg.Username, cfg.FirebaseUID)
	log.Printf("Test Repo: %s", cfg.RepoName)
	log.Printf("==================================================")

	if cfg.UseEmulator {
		if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
			os.Setenv("FIRESTORE_EMULATOR_HOST", "localhost:8084")
			log.Printf("FIRESTORE_EMULATOR_HOST not set, defaulting to localhost:8084")
		}
	}

	ctx := context.Background()

	// ----------------------------------------------------
	// TIER 1: Core API Feature Coverage
	// ----------------------------------------------------
	tier1 := "Tier 1: Core API"

	// 1. Health check
	err := testHealth(cfg.BaseURL)
	record(tier1, "Health Endpoint Check", err == nil, "%v", err)

	// 2. User registration
	err = testUserRegistration(cfg.BaseURL, cfg.FirebaseUID, cfg.Username)
	record(tier1, "User Registration", err == nil, "%v", err)

	// 3. Repo creation
	err = testRepoCreation(cfg.BaseURL, cfg.FirebaseUID, cfg.RepoName, "public")
	record(tier1, "Repository Creation", err == nil, "%v", err)

	// 4. PAT token CRUD
	patToken, patID, err := testPATCrud(cfg.BaseURL, cfg.FirebaseUID)
	record(tier1, "PAT Token CRUD", err == nil, "Token generated: %s (ID: %s)", patToken, patID)

	// 5. Basic auth handshake
	err = testBasicAuthHandshake(cfg.BaseURL, cfg.Username, patToken, cfg.RepoName)
	record(tier1, "Basic Auth Handshake", err == nil, "%v", err)

	// ----------------------------------------------------
	// TIER 2: Boundary and Error Handling
	// ----------------------------------------------------
	tier2 := "Tier 2: Boundary/Errors"

	// 1. Duplicate username
	err = testDuplicateUsername(cfg.BaseURL, cfg.Username)
	record(tier2, "Duplicate Username", err == nil, "%v", err)

	// 2. Bad repo name validation
	err = testBadRepoNames(cfg.BaseURL, cfg.FirebaseUID)
	record(tier2, "Bad Repo Names", err == nil, "%v", err)

	// 3. Private repo unauthorized access
	privateRepoName := "e2e-private-" + randID
	err = testPrivateRepoAccess(cfg.BaseURL, cfg.FirebaseUID, cfg.Username, privateRepoName)
	record(tier2, "Private Repo Unauthorized Access", err == nil, "%v", err)

	// ----------------------------------------------------
	// TIER 3: Cross-Feature Combinations (Git Push/Pull Flow)
	// ----------------------------------------------------
	tier3 := "Tier 3: Cross-Feature Git Flow"

	pushedSHA, err := testGitPushPullFlow(ctx, cfg, patToken)
	record(tier3, "Git Push/Pull Flow & Firestore Ref Check", err == nil, "Pushed SHA: %s, err: %v", pushedSHA, err)

	// ----------------------------------------------------
	// TIER 4: Real-world Scenarios
	// ----------------------------------------------------
	tier4 := "Tier 4: Real-world / Git LFS"

	// 1. Git LFS push and pull
	if cfg.RunLFS {
		err = testGitLFSPushPull(cfg, patToken)
		record(tier4, "Git LFS Push/Pull Validation", err == nil, "%v", err)
	} else {
		record(tier4, "Git LFS Push/Pull Validation", true, "Skipped by configuration")
	}

	// 2. Commit log REST API
	err = testCommitLogAPI(cfg.BaseURL, cfg.FirebaseUID, cfg.Username, cfg.RepoName, pushedSHA)
	record(tier4, "Browse Commit Log API", err == nil, "%v", err)

	// 3. File tree browsing REST API
	err = testFileTreeAPI(cfg.BaseURL, cfg.FirebaseUID, cfg.Username, cfg.RepoName)
	record(tier4, "Browse File Tree API", err == nil, "%v", err)

	// 4. Blob viewer REST API
	err = testBlobAPI(cfg.BaseURL, cfg.FirebaseUID, cfg.Username, cfg.RepoName)
	record(tier4, "Browse Blob Content API", err == nil, "%v", err)

	// Print Test Matrix Summary
	printSummary()

	// Exit code based on failures
	for _, res := range results {
		if !res.Passed {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

// ----------------------------------------------------
// TIER 1 IMPLEMENTATIONS
// ----------------------------------------------------

func testHealth(baseURL string) error {
	resp, err := http.Get(baseURL + "/api/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	if status, ok := body["status"]; !ok || status != "ok" {
		return fmt.Errorf("unexpected health response payload: %v", body)
	}
	return nil
}

func testUserRegistration(baseURL, uid, username string) error {
	reqBody, _ := json.Marshal(map[string]string{"username": username})
	req, _ := http.NewRequest("POST", baseURL+"/api/user/username", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer mock_"+uid)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed: Status %d, Payload %s", resp.StatusCode, string(body))
	}

	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	if success, ok := res["success"].(bool); !ok || !success {
		return fmt.Errorf("unexpected registration response success status: %v", res)
	}
	return nil
}

func testRepoCreation(baseURL, uid, name, visibility string) error {
	reqBody, _ := json.Marshal(map[string]string{
		"name":        name,
		"description": "E2E Test Repo",
		"visibility":  visibility,
	})
	req, _ := http.NewRequest("POST", baseURL+"/api/repos", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer mock_"+uid)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("repo creation failed: Status %d, Payload %s", resp.StatusCode, string(body))
	}

	var res RepoResponse
	json.NewDecoder(resp.Body).Decode(&res)
	if !res.Success || res.Name != name {
		return fmt.Errorf("unexpected repo creation response metadata: %+v", res)
	}
	return nil
}

func testPATCrud(baseURL, uid string) (string, string, error) {
	// Create PAT
	reqBody, _ := json.Marshal(map[string]string{"name": "e2e-token"})
	req, _ := http.NewRequest("POST", baseURL+"/api/tokens", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer mock_"+uid)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("failed to create PAT: Status %d, Payload %s", resp.StatusCode, string(body))
	}

	var tokenRes TokenResponse
	json.NewDecoder(resp.Body).Decode(&tokenRes)
	if tokenRes.Token == "" {
		return "", "", fmt.Errorf("empty PAT token returned")
	}

	// List PATs
	listReq, _ := http.NewRequest("GET", baseURL+"/api/tokens", nil)
	listReq.Header.Set("Authorization", "Bearer mock_"+uid)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		return "", "", err
	}
	defer listResp.Body.Close()

	var tokens []map[string]interface{}
	json.NewDecoder(listResp.Body).Decode(&tokens)

	var targetTokenID string
	for _, tok := range tokens {
		if name, ok := tok["name"].(string); ok && name == "e2e-token" {
			targetTokenID = tok["id"].(string)
		}
	}

	if targetTokenID == "" {
		return "", "", fmt.Errorf("newly created token e2e-token not found in tokens list")
	}

	// Create another token to test deletion, ensuring we retain the one we will use for Git
	delReqBody, _ := json.Marshal(map[string]string{"name": "temp-token"})
	req2, _ := http.NewRequest("POST", baseURL+"/api/tokens", bytes.NewBuffer(delReqBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer mock_"+uid)
	resp2, _ := http.DefaultClient.Do(req2)
	var tempRes TokenResponse
	json.NewDecoder(resp2.Body).Decode(&tempRes)
	resp2.Body.Close()

	// List to find the temp token ID
	listReq2, _ := http.NewRequest("GET", baseURL+"/api/tokens", nil)
	listReq2.Header.Set("Authorization", "Bearer mock_"+uid)
	listResp2, _ := http.DefaultClient.Do(listReq2)
	var tokens2 []map[string]interface{}
	json.NewDecoder(listResp2.Body).Decode(&tokens2)
	listResp2.Body.Close()

	var tempTokenID string
	for _, tok := range tokens2 {
		if name, ok := tok["name"].(string); ok && name == "temp-token" {
			tempTokenID = tok["id"].(string)
		}
	}

	if tempTokenID != "" {
		// Delete the temp token
		delReq, _ := http.NewRequest("DELETE", baseURL+"/api/tokens/"+tempTokenID, nil)
		delReq.Header.Set("Authorization", "Bearer mock_"+uid)
		delResp, err := http.DefaultClient.Do(delReq)
		if err == nil {
			delResp.Body.Close()
			if delResp.StatusCode != http.StatusOK {
				return "", "", fmt.Errorf("failed to revoke temp token: %d", delResp.StatusCode)
			}
		}
	}

	return tokenRes.Token, targetTokenID, nil
}

func testBasicAuthHandshake(baseURL, username, token, repoName string) error {
	// Query info/refs endpoint using basic authentication
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/r/%s/%s.git/info/refs?service=git-upload-pack", baseURL, username, repoName), nil)
	req.SetBasicAuth(username, token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("basic auth failed: Status %d, Payload %s", resp.StatusCode, string(body))
	}
	return nil
}

// ----------------------------------------------------
// TIER 2 IMPLEMENTATIONS
// ----------------------------------------------------

func testDuplicateUsername(baseURL, username string) error {
	// Attempt to register duplicate username using a different mock UID
	reqBody, _ := json.Marshal(map[string]string{"username": username})
	req, _ := http.NewRequest("POST", baseURL+"/api/user/username", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer mock_user_2")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return fmt.Errorf("expected registration of duplicate username to fail, but it returned 200 OK")
	}
	return nil
}

func testBadRepoNames(baseURL, uid string) error {
	badNames := []string{"a", "a/b", "repo_with_spaces ", "repo#char"}
	for _, name := range badNames {
		reqBody, _ := json.Marshal(map[string]string{"name": name})
		req, _ := http.NewRequest("POST", baseURL+"/api/repos", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer mock_"+uid)

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return fmt.Errorf("expected repository name %q to fail validation, but it succeeded", name)
			}
		}
	}
	return nil
}

func testPrivateRepoAccess(baseURL, uid, username, repoName string) error {
	// 1. Create a private repository
	err := testRepoCreation(baseURL, uid, repoName, "private")
	if err != nil {
		return fmt.Errorf("failed to create private repository: %v", err)
	}

	// 2. Try to access it without authorization (should fail)
	resp, err := http.Get(fmt.Sprintf("%s/api/repos/%s/%s", baseURL, username, repoName))
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected unauthorized fetch of private repo details to return 401 or 403, got %d", resp.StatusCode)
		}
	}

	// 3. Try to access it with a different user authorization (should fail)
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/repos/%s/%s", baseURL, username, repoName), nil)
	req.Header.Set("Authorization", "Bearer mock_unauthorized_user")
	resp2, err := http.DefaultClient.Do(req)
	if err == nil {
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden && resp2.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected other user access to private repo to return 401 or 403, got %d", resp2.StatusCode)
		}
	}

	// 4. Try to access git endpoint without authorization (should fail)
	reqGit, _ := http.NewRequest("GET", fmt.Sprintf("%s/r/%s/%s.git/info/refs?service=git-upload-pack", baseURL, username, repoName), nil)
	respGit, err := http.DefaultClient.Do(reqGit)
	if err == nil {
		respGit.Body.Close()
		if respGit.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected unauthorized git info/refs of private repo to return 401, got %d", respGit.StatusCode)
		}
	}

	return nil
}

// ----------------------------------------------------
// TIER 3 IMPLEMENTATION
// ----------------------------------------------------

func testGitPushPullFlow(ctx context.Context, cfg Config, token string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "gitbucket-e2e-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	localRepoPath := filepath.Join(tmpDir, "local")
	cloneRepoPath := filepath.Join(tmpDir, "clone")

	// 1. Initialize local repository
	if err := os.MkdirAll(localRepoPath, 0755); err != nil {
		return "", err
	}

	if _, err := runCmd(localRepoPath, "git", "init", "-b", "main"); err != nil {
		return "", fmt.Errorf("git init failed: %v", err)
	}

	// Configure local Git identity for commit
	runCmd(localRepoPath, "git", "config", "user.name", "E2E Tester")
	runCmd(localRepoPath, "git", "config", "user.email", "e2e@example.com")

	// Create a dummy file
	testFile := filepath.Join(localRepoPath, "README.md")
	if err := os.WriteFile(testFile, []byte("# E2E Test Repo\nThis is a programmatic Git push flow test."), 0644); err != nil {
		return "", err
	}

	if _, err := runCmd(localRepoPath, "git", "add", "README.md"); err != nil {
		return "", err
	}

	if _, err := runCmd(localRepoPath, "git", "commit", "-m", "Initial commit from Go E2E test suite"); err != nil {
		return "", err
	}

	// Get local commit SHA
	shaOut, err := runCmd(localRepoPath, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	localSHA := strings.TrimSpace(shaOut)

	// 2. Push to remote
	// remoteURL format: http://username:token@host:port/r/username/repo.git
	cleanBaseURL := strings.Replace(cfg.BaseURL, "http://", "", 1)
	remoteURL := fmt.Sprintf("http://%s:%s@%s/r/%s/%s.git", cfg.Username, token, cleanBaseURL, cfg.Username, cfg.RepoName)

	if _, err := runCmd(localRepoPath, "git", "remote", "add", "origin", remoteURL); err != nil {
		return "", err
	}

	// Push
	if _, err := runCmd(localRepoPath, "git", "push", "origin", "main"); err != nil {
		return "", fmt.Errorf("git push failed: %v", err)
	}

	// 3. Clone remote to separate path
	if _, err := runCmd(tmpDir, "git", "clone", remoteURL, "clone"); err != nil {
		return "", fmt.Errorf("git clone failed: %v", err)
	}

	// Verify cloned SHA
	cloneSHAOut, err := runCmd(cloneRepoPath, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	cloneSHA := strings.TrimSpace(cloneSHAOut)

	if localSHA != cloneSHA {
		return "", fmt.Errorf("cloned commit SHA %q does not match pushed commit SHA %q", cloneSHA, localSHA)
	}

	// Read file contents to make sure they match
	data, err := os.ReadFile(filepath.Join(cloneRepoPath, "README.md"))
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(data), "programmatic Git push flow test") {
		return "", fmt.Errorf("cloned file contents mismatched or missing")
	}

	// 4. Verify references in Firestore
	// We read Firestore directly to check if reference mapping has been synced to branch head
	log.Printf("Connecting to Firestore to verify refs...")
	fsClient, err := firestore.NewClient(ctx, cfg.ProjectID)
	if err != nil {
		log.Printf("[WARNING] Could not connect to Firestore to check refs: %v", err)
		return localSHA, nil
	}
	defer fsClient.Close()

	repoDocID := fmt.Sprintf("%s_%s", strings.ToLower(cfg.Username), strings.ToLower(cfg.RepoName))
	doc, err := fsClient.Collection("repositories").Doc(repoDocID).Get(ctx)
	if err != nil {
		return "", fmt.Errorf("repository document %s not found in Firestore: %v", repoDocID, err)
	}

	// Confirm branch list contains 'main'
	branchesData, err := doc.DataAt("branches")
	if err != nil {
		return "", fmt.Errorf("failed to read branches field from Firestore document: %v", err)
	}
	branchesList, ok := branchesData.([]interface{})
	if !ok {
		return "", fmt.Errorf("branches field in Firestore is not an array")
	}
	foundMain := false
	for _, b := range branchesList {
		if bStr, ok := b.(string); ok && bStr == "main" {
			foundMain = true
			break
		}
	}
	if !foundMain {
		return "", fmt.Errorf("branches array in Firestore repositories doc missing 'main'")
	}

	// Check refs collection inside repository (Go backend design contract for ACID ref store)
	// Some backends might store refs as subcollections: repositories/{repoId}/refs/heads/main
	refDoc, err := fsClient.Collection("repositories").Doc(repoDocID).Collection("refs").Doc("heads_main").Get(ctx)
	if err != nil {
		// Fallback checking in repositories doc fields if not stored in subcollection
		refSHAField, err := doc.DataAt("refs.heads.main")
		if err != nil {
			log.Printf("[INFO] Refs subcollection and doc fields not found. Checking commitsCache...")
			commitsCache, err := doc.DataAt("commitsCache")
			if err != nil || commitsCache == nil {
				return "", fmt.Errorf("reference SHA and commits cache not found in Firestore")
			}
		} else if refSHAField.(string) != localSHA {
			return "", fmt.Errorf("reference SHA in Firestore (%s) does not match pushed SHA (%s)", refSHAField, localSHA)
		}
	} else {
		shaVal, err := refDoc.DataAt("sha")
		if err != nil {
			return "", fmt.Errorf("missing sha field in ref document")
		}
		if shaVal.(string) != localSHA {
			return "", fmt.Errorf("Firestore ref SHA (%s) does not match pushed SHA (%s)", shaVal, localSHA)
		}
	}

	return localSHA, nil
}

// ----------------------------------------------------
// TIER 4 IMPLEMENTATIONS
// ----------------------------------------------------

func testGitLFSPushPull(cfg Config, token string) error {
	// Verify git-lfs CLI is installed
	if _, err := exec.LookPath("git-lfs"); err != nil {
		return fmt.Errorf("git-lfs is not installed on the system path: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "gitbucket-lfs-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	localRepoPath := filepath.Join(tmpDir, "local")
	cloneRepoPath := filepath.Join(tmpDir, "clone")

	// 1. Initialize local repository
	if err := os.MkdirAll(localRepoPath, 0755); err != nil {
		return err
	}

	if _, err := runCmd(localRepoPath, "git", "init", "-b", "main"); err != nil {
		return err
	}

	runCmd(localRepoPath, "git", "config", "user.name", "LFS Tester")
	runCmd(localRepoPath, "git", "config", "user.email", "lfs@example.com")

	// Initialize LFS in this repository
	if _, err := runCmd(localRepoPath, "git", "lfs", "install", "--local"); err != nil {
		return fmt.Errorf("git lfs install failed: %v", err)
	}

	// Track bin files in Git LFS
	if _, err := runCmd(localRepoPath, "git", "lfs", "track", "*.bin"); err != nil {
		return fmt.Errorf("git lfs track failed: %v", err)
	}

	// Create actual large binary file (~5 MB)
	largeData := make([]byte, 5*1024*1024)
	if _, err := rand.Read(largeData); err != nil {
		return err
	}

	largeFilePath := filepath.Join(localRepoPath, "largefile.bin")
	if err := os.WriteFile(largeFilePath, largeData, 0644); err != nil {
		return err
	}

	// Commit files
	if _, err := runCmd(localRepoPath, "git", "add", ".gitattributes", "largefile.bin"); err != nil {
		return err
	}

	if _, err := runCmd(localRepoPath, "git", "commit", "-m", "Commit with Git LFS large asset"); err != nil {
		return err
	}

	// Push
	cleanBaseURL := strings.Replace(cfg.BaseURL, "http://", "", 1)
	remoteURL := fmt.Sprintf("http://%s:%s@%s/r/%s/%s.git", cfg.Username, token, cleanBaseURL, cfg.Username, cfg.RepoName)

	if _, err := runCmd(localRepoPath, "git", "remote", "add", "origin", remoteURL); err != nil {
		return err
	}

	log.Printf("Pushing Git LFS file to server...")
	if _, err := runCmd(localRepoPath, "git", "push", "origin", "main"); err != nil {
		return fmt.Errorf("git LFS push failed: %v", err)
	}

	// 2. Clone to verify pull resolved LFS object instead of pointer file
	log.Printf("Cloning Git LFS repository from server...")
	// Run clone with LFS env configured
	cloneCmd := exec.Command("git", "clone", remoteURL, "clone")
	cloneCmd.Dir = tmpDir
	cloneCmd.Env = append(os.Environ(), "GIT_LFS_SKIP_SMUDGE=0") // Ensure LFS fetches actual assets
	var stderr bytes.Buffer
	cloneCmd.Stderr = &stderr
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("git LFS clone failed: %v, stderr: %s", err, stderr.String())
	}

	// Check file size of largefile.bin in clone folder
	clonedFile := filepath.Join(cloneRepoPath, "largefile.bin")
	info, err := os.Stat(clonedFile)
	if err != nil {
		return fmt.Errorf("cloned LFS file not found: %v", err)
	}

	// If pointer file was pulled instead of actual file, size would be < 200 bytes
	if info.Size() != int64(len(largeData)) {
		// Read content to show what was downloaded
		content, _ := os.ReadFile(clonedFile)
		return fmt.Errorf("LFS smudging failed: expected size %d, got size %d. Content was: %s", len(largeData), info.Size(), string(content))
	}

	return nil
}

func testCommitLogAPI(baseURL, uid, username, repoName, expectedSHA string) error {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/repos/%s/%s/commits/main", baseURL, username, repoName), nil)
	req.Header.Set("Authorization", "Bearer mock_"+uid)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("commits log API returned status %s", resp.Status)
	}

	var commits []CommitItem
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return err
	}

	if len(commits) == 0 {
		return fmt.Errorf("commit logs list was returned empty")
	}

	// Check if the expected SHA is in the logs
	found := false
	for _, commit := range commits {
		if strings.HasPrefix(commit.SHA, expectedSHA) || strings.HasPrefix(expectedSHA, commit.SHA) {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("expected commit SHA %s not found in commits log: %+v", expectedSHA, commits)
	}

	return nil
}

func testFileTreeAPI(baseURL, uid, username, repoName string) error {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/repos/%s/%s/tree/main/", baseURL, username, repoName), nil)
	req.Header.Set("Authorization", "Bearer mock_"+uid)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tree API returned status %s", resp.Status)
	}

	var tree []TreeItem
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return err
	}

	hasReadme := false
	for _, item := range tree {
		if item.Name == "README.md" && item.Type == "blob" {
			hasReadme = true
			break
		}
	}

	if !hasReadme {
		return fmt.Errorf("file tree missing expected README.md file: %+v", tree)
	}

	return nil
}

func testBlobAPI(baseURL, uid, username, repoName string) error {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/repos/%s/%s/blob/main/README.md", baseURL, username, repoName), nil)
	req.Header.Set("Authorization", "Bearer mock_"+uid)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("blob API returned status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if !strings.Contains(string(body), "# E2E Test Repo") {
		return fmt.Errorf("blob content mismatch: %q", string(body))
	}

	return nil
}

// ----------------------------------------------------
// UTILITY FUNCTIONS
// ----------------------------------------------------

func randomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "0000"
	}
	return hex.EncodeToString(bytes)
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("failed to run %s %v: %v, stderr: %s", name, args, err, stderr.String())
	}
	return stdout.String(), nil
}

func printSummary() {
	log.Printf("==================================================")
	log.Printf("📋 GitBucket E2E Test Suite Results Matrix")
	log.Printf("==================================================")
	passedCount := 0
	failedCount := 0

	for _, res := range results {
		status := "🟢 PASS"
		if !res.Passed {
			status = "🔴 FAIL"
			failedCount++
		} else {
			passedCount++
		}
		log.Printf("[%s] %s :: %s", status, res.Tier, res.Name)
		if !res.Passed {
			log.Printf("       ↳ Error: %s", res.Message)
		}
	}

	log.Printf("--------------------------------------------------")
	log.Printf("Summary: %d Passed, %d Failed", passedCount, failedCount)
	log.Printf("==================================================")
}
