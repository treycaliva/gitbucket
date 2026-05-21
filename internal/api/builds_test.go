package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
	"gitbucket/internal/sync"
)

func TestBuildsAPI(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping Builds API tests: FIRESTORE_EMULATOR_HOST not set")
	}

	ctx := context.Background()
	client, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}
	defer client.Close()

	os.Setenv("DEV_MODE", "true")
	defer os.Unsetenv("DEV_MODE")

	authHandler := auth.NewAuthHandler(true, nil, client)
	apiHandler := NewAPIHandler(client, nil, "", "/tmp/gitbucket-test-repos", authHandler, sync.NewKMSClient(nil, ""))

	r := chi.NewRouter()
	apiHandler.RegisterRoutes(r)

	suffix := randSuffix()
	owner := "owner-" + suffix
	repo := "repo-" + suffix
	uid := "uid-" + suffix

	// 1. Setup repository in Firestore
	repoRef := client.Collection("repositories").Doc(owner + "_" + repo)
	_, err = repoRef.Set(ctx, map[string]interface{}{
		"id":        owner + "_" + repo,
		"name":      repo,
		"owner":     owner,
		"ownerUid":  uid,
		"isPublic":  false,
		"createdAt": time.Now(),
		"updatedAt": time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to set up test repository metadata: %v", err)
	}
	defer repoRef.Delete(ctx)

	// 2. Setup user in Firestore (so GetUID/GetUsername works correctly)
	userRef := client.Collection("users").Doc(uid)
	_, err = userRef.Set(ctx, map[string]interface{}{
		"uid":      uid,
		"username": owner,
	})
	if err != nil {
		t.Fatalf("failed to set up test user: %v", err)
	}
	defer userRef.Delete(ctx)

	buildID := "build-123"
	sha := "sha-123456"

	// 3. Test Webhook POST /api/webhooks/cloudbuild
	webhookPayload := map[string]interface{}{
		"message": map[string]interface{}{
			"data": []byte(fmt.Sprintf(`{
				"id": "%s",
				"status": "QUEUED",
				"updateTime": "%s",
				"substitutions": {
					"_REPO_OWNER": "%s",
					"_REPO_NAME": "%s",
					"_COMMIT_SHA": "%s"
				}
			}`, buildID, time.Now().Format(time.RFC3339), owner, repo, sha)),
		},
	}
	reqBody, _ := json.Marshal(webhookPayload)
	reqWebhook := httptest.NewRequest("POST", "/api/webhooks/cloudbuild", bytes.NewBuffer(reqBody))
	rrWebhook := httptest.NewRecorder()
	r.ServeHTTP(rrWebhook, reqWebhook)

	if rrWebhook.Code != http.StatusOK {
		t.Fatalf("expected webhook status 200 OK, got %d (body: %s)", rrWebhook.Code, rrWebhook.Body.String())
	}

	// Verify Firestore status doc
	statusID := fmt.Sprintf("%s_%s_%s_%s", owner, repo, sha, buildID)
	statusRef := client.Collection("commit_statuses").Doc(statusID)
	defer statusRef.Delete(ctx)

	doc, err := statusRef.Get(ctx)
	if err != nil {
		t.Fatalf("expected commit status doc to exist: %v", err)
	}
	var statusData map[string]interface{}
	doc.DataTo(&statusData)
	if statusData["status"] != "QUEUED" {
		t.Errorf("expected status 'QUEUED', got %v", statusData["status"])
	}

	// 4. Test Ticket creation POST /api/repos/{owner}/{repo}/builds/ticket
	ticketPayload := map[string]string{
		"buildId": buildID,
	}
	tBody, _ := json.Marshal(ticketPayload)
	reqTicket := httptest.NewRequest("POST", fmt.Sprintf("/api/repos/%s/%s/builds/ticket", owner, repo), bytes.NewBuffer(tBody))
	reqTicket.Header.Set("Authorization", "Bearer mock_"+uid)
	rrTicket := httptest.NewRecorder()
	r.ServeHTTP(rrTicket, reqTicket)

	if rrTicket.Code != http.StatusOK {
		t.Fatalf("expected ticket status 200 OK, got %d (body: %s)", rrTicket.Code, rrTicket.Body.String())
	}

	var ticketResp map[string]string
	json.NewDecoder(rrTicket.Body).Decode(&ticketResp)
	ticketToken := ticketResp["ticket"]
	if ticketToken == "" {
		t.Error("expected ticket token to be generated and non-empty")
	}

	// Verify the ticket parses and validates
	claims, err := VerifyTicketToken(ticketToken)
	if err != nil {
		t.Fatalf("failed to verify ticket token: %v", err)
	}
	if claims.BuildID != buildID || claims.Owner != owner || claims.Repo != repo {
		t.Errorf("ticket claim mismatch: %+v", claims)
	}

	// 5. Test Signed URL creation GET /api/repos/{owner}/{repo}/builds/{buildId}/logs
	reqLogs := httptest.NewRequest("GET", fmt.Sprintf("/api/repos/%s/%s/builds/%s/logs", owner, repo, buildID), nil)
	reqLogs.Header.Set("Authorization", "Bearer mock_"+uid)
	rrLogs := httptest.NewRecorder()
	r.ServeHTTP(rrLogs, reqLogs)

	if rrLogs.Code != http.StatusOK {
		t.Fatalf("expected logs status 200 OK, got %d (body: %s)", rrLogs.Code, rrLogs.Body.String())
	}

	var logsResp map[string]string
	json.NewDecoder(rrLogs.Body).Decode(&logsResp)
	signedUrl := logsResp["signedUrl"]
	if signedUrl == "" {
		t.Error("expected signed URL to be generated and non-empty")
	}
	if !strings.Contains(signedUrl, "mock-signed-url") {
		t.Errorf("expected mock signed URL, got %q", signedUrl)
	}
}
