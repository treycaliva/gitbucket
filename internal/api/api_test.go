package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestAPIHandler(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping API handler tests: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer client.Close()

	authHandler := auth.NewAuthHandler(true, nil, client) // DevMode = true, using firestore client
	apiHandler := NewAPIHandler(client, nil, "", "", authHandler)

	r := chi.NewRouter()
	apiHandler.RegisterRoutes(r)

	suffix := randSuffix()
	uid := "api-uid-" + suffix
	username := "api-user-" + suffix

	// 1. Test registration validation failures (bad format)
	badUsernames := []string{"ab", "invalid_username!", "a-very-long-username-that-exceeds-twenty-chars"}
	for _, badUser := range badUsernames {
		reqBody, _ := json.Marshal(map[string]string{"username": badUser})
		req := httptest.NewRequest("POST", "/api/user/username", bytes.NewBuffer(reqBody))
		req.Header.Set("Authorization", "Bearer mock_"+uid)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400 Bad Request for username %q, got %d", badUser, rr.Code)
		}
	}

	// 2. Test successful registration
	reqBody, _ := json.Marshal(map[string]string{"username": username})
	req := httptest.NewRequest("POST", "/api/user/username", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for valid registration, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	var regResp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&regResp)
	if success, ok := regResp["success"].(bool); !ok || !success {
		t.Errorf("expected success to be true, got %v", regResp)
	}
	if regResp["username"] != username {
		t.Errorf("expected registered username to be %q, got %v", username, regResp["username"])
	}

	// 3. Test duplicate registration (should fail)
	req2 := httptest.NewRequest("POST", "/api/user/username", bytes.NewBuffer(reqBody))
	req2.Header.Set("Authorization", "Bearer mock_other_uid_"+suffix)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code == http.StatusOK {
		t.Error("expected duplicate username registration to fail, but got 200 OK")
	}

	// 4. Test GET /api/user/me
	reqMe := httptest.NewRequest("GET", "/api/user/me", nil)
	reqMe.Header.Set("Authorization", "Bearer mock_"+uid)
	rrMe := httptest.NewRecorder()
	r.ServeHTTP(rrMe, reqMe)
	if rrMe.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for GET profile, got %d", rrMe.Code)
	}

	var meResp map[string]interface{}
	json.NewDecoder(rrMe.Body).Decode(&meResp)
	if meResp["uid"] != uid {
		t.Errorf("expected profile uid %q, got %v", uid, meResp["uid"])
	}
	if meResp["username"] != username {
		t.Errorf("expected profile username %q, got %v", username, meResp["username"])
	}

	// 5. Test Generate PAT (POST /api/tokens)
	tokBody, _ := json.Marshal(map[string]string{"name": "test-token-" + suffix})
	reqTok := httptest.NewRequest("POST", "/api/tokens", bytes.NewBuffer(tokBody))
	reqTok.Header.Set("Authorization", "Bearer mock_"+uid)
	rrTok := httptest.NewRecorder()
	r.ServeHTTP(rrTok, reqTok)

	if rrTok.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for token generation, got %d", rrTok.Code)
	}

	var tokResp map[string]string
	json.NewDecoder(rrTok.Body).Decode(&tokResp)
	rawToken := tokResp["token"]
	if !strings.HasPrefix(rawToken, "gb_pat_") {
		t.Errorf("expected generated token to start with gb_pat_, got %q", rawToken)
	}

	// 6. Test List PATs (GET /api/tokens)
	reqList := httptest.NewRequest("GET", "/api/tokens", nil)
	reqList.Header.Set("Authorization", "Bearer mock_"+uid)
	rrList := httptest.NewRecorder()
	r.ServeHTTP(rrList, reqList)

	if rrList.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for listing tokens, got %d", rrList.Code)
	}

	var listResp []map[string]interface{}
	json.NewDecoder(rrList.Body).Decode(&listResp)
	if len(listResp) != 1 {
		t.Fatalf("expected 1 token in list, got %d", len(listResp))
	}
	if listResp[0]["name"] != "test-token-"+suffix {
		t.Errorf("expected token name 'test-token-%s', got %v", suffix, listResp[0]["name"])
	}
	tokenId := listResp[0]["id"].(string)

	// 7. Test Revoke PAT (DELETE /api/tokens/{tokenId})
	reqDel := httptest.NewRequest("DELETE", "/api/tokens/"+tokenId, nil)
	reqDel.Header.Set("Authorization", "Bearer mock_"+uid)
	rrDel := httptest.NewRecorder()
	r.ServeHTTP(rrDel, reqDel)

	if rrDel.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for token revocation, got %d", rrDel.Code)
	}

	// 8. Verify the token was indeed revoked by listing again
	rrList2 := httptest.NewRecorder()
	r.ServeHTTP(rrList2, reqList)
	var listResp2 []map[string]interface{}
	json.NewDecoder(rrList2.Body).Decode(&listResp2)
	if len(listResp2) != 0 {
		t.Errorf("expected 0 tokens after revocation, got %d", len(listResp2))
	}
}
