package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestGitAndRepositoryAPIs(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping API integration tests: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer client.Close()

	// Setup local repos root directory for the test
	tempReposRoot, err := os.MkdirTemp("", "gitbucket-test-repos-*")
	if err != nil {
		t.Fatalf("failed to create temp repos root: %v", err)
	}
	defer os.RemoveAll(tempReposRoot)

	authHandler := auth.NewAuthHandler(true, nil, client)
	apiHandler := NewAPIHandler(client, nil, "", tempReposRoot, authHandler)

	r := chi.NewRouter()
	apiHandler.RegisterRoutes(r)

	suffix := randSuffix()
	uid := "git-uid-" + suffix
	username := "git-user-" + suffix

	// Register username
	regBody, _ := json.Marshal(map[string]string{"username": username})
	req := httptest.NewRequest("POST", "/api/user/username", bytes.NewBuffer(regBody))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("failed to register user in test: %d (body: %s)", rr.Code, rr.Body.String())
	}

	// Generate PAT
	tokBody, _ := json.Marshal(map[string]string{"name": "test-pat-" + suffix})
	reqTok := httptest.NewRequest("POST", "/api/tokens", bytes.NewBuffer(tokBody))
	reqTok.Header.Set("Authorization", "Bearer mock_"+uid)
	rrTok := httptest.NewRecorder()
	r.ServeHTTP(rrTok, reqTok)
	if rrTok.Code != http.StatusOK {
		t.Fatalf("failed to generate PAT in test: %d (body: %s)", rrTok.Code, rrTok.Body.String())
	}

	var tokResp map[string]string
	json.NewDecoder(rrTok.Body).Decode(&tokResp)
	patToken := tokResp["token"]

	// 1. Create a public repository
	pubRepoBody, _ := json.Marshal(map[string]string{
		"name":        "pubrepo-" + suffix,
		"description": "Public Test Repo",
		"visibility":  "public",
	})
	reqPubRepo := httptest.NewRequest("POST", "/api/repos", bytes.NewBuffer(pubRepoBody))
	reqPubRepo.Header.Set("Authorization", "Bearer mock_"+uid)
	rrPubRepo := httptest.NewRecorder()
	r.ServeHTTP(rrPubRepo, reqPubRepo)
	if rrPubRepo.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public repo creation, got %d (body: %s)", rrPubRepo.Code, rrPubRepo.Body.String())
	}

	// 2. Create a private repository
	privRepoBody, _ := json.Marshal(map[string]string{
		"name":        "privrepo-" + suffix,
		"description": "Private Test Repo",
		"visibility":  "private",
	})
	reqPrivRepo := httptest.NewRequest("POST", "/api/repos", bytes.NewBuffer(privRepoBody))
	reqPrivRepo.Header.Set("Authorization", "Bearer mock_"+uid)
	rrPrivRepo := httptest.NewRecorder()
	r.ServeHTTP(rrPrivRepo, reqPrivRepo)
	if rrPrivRepo.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for private repo creation, got %d (body: %s)", rrPrivRepo.Code, rrPrivRepo.Body.String())
	}

	// 3. List repositories without authentication (optional auth - public only)
	reqListPub := httptest.NewRequest("GET", "/api/repos", nil)
	rrListPub := httptest.NewRecorder()
	r.ServeHTTP(rrListPub, reqListPub)
	if rrListPub.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for listing public repos, got %d", rrListPub.Code)
	}

	var pubList []map[string]interface{}
	json.NewDecoder(rrListPub.Body).Decode(&pubList)

	hasPub := false
	hasPriv := false
	for _, rp := range pubList {
		name, _ := rp["name"].(string)
		if name == "pubrepo-"+suffix {
			hasPub = true
		}
		if name == "privrepo-"+suffix {
			hasPriv = true
		}
	}

	if !hasPub {
		t.Errorf("expected public repo 'pubrepo-%s' to be in public repositories list", suffix)
	}
	if hasPriv {
		t.Errorf("expected private repo 'privrepo-%s' to be excluded from public repositories list", suffix)
	}

	// 4. List repositories with authentication (optional auth - both public and user's private)
	reqListAll := httptest.NewRequest("GET", "/api/repos", nil)
	reqListAll.Header.Set("Authorization", "Bearer mock_"+uid)
	rrListAll := httptest.NewRecorder()
	r.ServeHTTP(rrListAll, reqListAll)
	if rrListAll.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for listing all repos, got %d", rrListAll.Code)
	}

	var allList []map[string]interface{}
	json.NewDecoder(rrListAll.Body).Decode(&allList)

	hasPub = false
	hasPriv = false
	for _, rp := range allList {
		name, _ := rp["name"].(string)
		if name == "pubrepo-"+suffix {
			hasPub = true
		}
		if name == "privrepo-"+suffix {
			hasPriv = true
		}
	}

	if !hasPub {
		t.Errorf("expected public repo 'pubrepo-%s' to be in authenticated repositories list", suffix)
	}
	if !hasPriv {
		t.Errorf("expected private repo 'privrepo-%s' to be in authenticated repositories list", suffix)
	}

	// 5. Get single repository metadata
	// 5a. Public repo metadata (unauthenticated)
	reqGetPub := httptest.NewRequest("GET", "/api/repos/"+username+"/pubrepo-"+suffix, nil)
	rrGetPub := httptest.NewRecorder()
	r.ServeHTTP(rrGetPub, reqGetPub)
	if rrGetPub.Code != http.StatusOK {
		t.Errorf("expected 200 OK for getting public repo, got %d", rrGetPub.Code)
	}

	// 5b. Private repo metadata (unauthenticated -> 401 Unauthorized)
	reqGetPrivUnauth := httptest.NewRequest("GET", "/api/repos/"+username+"/privrepo-"+suffix, nil)
	rrGetPrivUnauth := httptest.NewRecorder()
	r.ServeHTTP(rrGetPrivUnauth, reqGetPrivUnauth)
	if rrGetPrivUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for private repo without auth, got %d", rrGetPrivUnauth.Code)
	}

	// 5c. Private repo metadata (wrong user auth -> 403 Forbidden)
	reqGetPrivWrong := httptest.NewRequest("GET", "/api/repos/"+username+"/privrepo-"+suffix, nil)
	reqGetPrivWrong.Header.Set("Authorization", "Bearer mock_otheruid")
	rrGetPrivWrong := httptest.NewRecorder()
	r.ServeHTTP(rrGetPrivWrong, reqGetPrivWrong)
	if rrGetPrivWrong.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for private repo with wrong auth, got %d", rrGetPrivWrong.Code)
	}

	// 5d. Private repo metadata (correct auth -> 200 OK)
	reqGetPrivCorrect := httptest.NewRequest("GET", "/api/repos/"+username+"/privrepo-"+suffix, nil)
	reqGetPrivCorrect.Header.Set("Authorization", "Bearer mock_"+uid)
	rrGetPrivCorrect := httptest.NewRecorder()
	r.ServeHTTP(rrGetPrivCorrect, reqGetPrivCorrect)
	if rrGetPrivCorrect.Code != http.StatusOK {
		t.Errorf("expected 200 OK for private repo with owner auth, got %d", rrGetPrivCorrect.Code)
	}

	// 6. Test Git Smart HTTP info/refs endpoints
	// 6a. Public clone info/refs (unauthenticated -> 200 OK)
	reqGitPubRefs := httptest.NewRequest("GET", "/r/"+username+"/pubrepo-"+suffix+".git/info/refs?service=git-upload-pack", nil)
	rrGitPubRefs := httptest.NewRecorder()
	r.ServeHTTP(rrGitPubRefs, reqGitPubRefs)
	if rrGitPubRefs.Code != http.StatusOK {
		t.Errorf("expected 200 OK for public git refs clone, got %d (body: %s)", rrGitPubRefs.Code, rrGitPubRefs.Body.String())
	}
	contentType := rrGitPubRefs.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/x-git-upload-pack-advertisement") {
		t.Errorf("expected content type to contain application/x-git-upload-pack-advertisement, got %q", contentType)
	}

	// 6b. Private clone info/refs (unauthenticated -> 401 Unauthorized)
	reqGitPrivRefsUnauth := httptest.NewRequest("GET", "/r/"+username+"/privrepo-"+suffix+".git/info/refs?service=git-upload-pack", nil)
	rrGitPrivRefsUnauth := httptest.NewRecorder()
	r.ServeHTTP(rrGitPrivRefsUnauth, reqGitPrivRefsUnauth)
	if rrGitPrivRefsUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for private git refs clone without auth, got %d", rrGitPrivRefsUnauth.Code)
	}

	// 6c. Private clone info/refs (correct PAT auth -> 200 OK)
	reqGitPrivRefsAuth := httptest.NewRequest("GET", "/r/"+username+"/privrepo-"+suffix+".git/info/refs?service=git-upload-pack", nil)
	reqGitPrivRefsAuth.SetBasicAuth(username, patToken)
	rrGitPrivRefsAuth := httptest.NewRecorder()
	r.ServeHTTP(rrGitPrivRefsAuth, reqGitPrivRefsAuth)
	if rrGitPrivRefsAuth.Code != http.StatusOK {
		t.Errorf("expected 200 OK for private git refs clone with PAT, got %d (body: %s)", rrGitPrivRefsAuth.Code, rrGitPrivRefsAuth.Body.String())
	}

	// 6d. Push info/refs (requester is owner -> 200 OK)
	reqGitPushRefsAuth := httptest.NewRequest("GET", "/r/"+username+"/pubrepo-"+suffix+".git/info/refs?service=git-receive-pack", nil)
	reqGitPushRefsAuth.SetBasicAuth(username, patToken)
	rrGitPushRefsAuth := httptest.NewRecorder()
	r.ServeHTTP(rrGitPushRefsAuth, reqGitPushRefsAuth)
	if rrGitPushRefsAuth.Code != http.StatusOK {
		t.Errorf("expected 200 OK for git refs push with owner PAT, got %d (body: %s)", rrGitPushRefsAuth.Code, rrGitPushRefsAuth.Body.String())
	}

	// 6e. Push info/refs (requester is not owner -> 401 Unauthorized)
	reqGitPushRefsWrong := httptest.NewRequest("GET", "/r/"+username+"/pubrepo-"+suffix+".git/info/refs?service=git-receive-pack", nil)
	reqGitPushRefsWrong.SetBasicAuth("otheruser", patToken)
	rrGitPushRefsWrong := httptest.NewRecorder()
	r.ServeHTTP(rrGitPushRefsWrong, reqGitPushRefsWrong)
	if rrGitPushRefsWrong.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for git refs push with wrong user, got %d", rrGitPushRefsWrong.Code)
	}

	// 7. Delete repository
	reqDelRepo := httptest.NewRequest("DELETE", "/api/repos/"+username+"/pubrepo-"+suffix, nil)
	reqDelRepo.Header.Set("Authorization", "Bearer mock_"+uid)
	rrDelRepo := httptest.NewRecorder()
	r.ServeHTTP(rrDelRepo, reqDelRepo)
	if rrDelRepo.Code != http.StatusOK {
		t.Errorf("expected 200 OK for repository deletion, got %d", rrDelRepo.Code)
	}

	// Verify it's deleted
	reqGetPubDeleted := httptest.NewRequest("GET", "/api/repos/"+username+"/pubrepo-"+suffix, nil)
	rrGetPubDeleted := httptest.NewRecorder()
	r.ServeHTTP(rrGetPubDeleted, reqGetPubDeleted)
	if rrGetPubDeleted.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for deleted public repo, got %d", rrGetPubDeleted.Code)
	}
}
