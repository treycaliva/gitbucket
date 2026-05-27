package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/firestore"
	loggingapiv2 "cloud.google.com/go/logging/apiv2"
	"cloud.google.com/go/logging/apiv2/loggingpb"
	"cloud.google.com/go/storage"
	credentials "cloud.google.com/go/iam/credentials/apiv1"
	credentialspb "google.golang.org/genproto/googleapis/iam/credentials/v1"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/idtoken"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for simplicity, since WebSocket is authenticated by ticket
	},
}

// Ticket token signing secret, regenerated on startup
var ticketSecret = make([]byte, 32)

func init() {
	_, _ = rand.Read(ticketSecret)
}

type TicketClaims struct {
	UserID    string
	Owner     string
	Repo      string
	BuildID   string
	ExpiresAt time.Time
}

// GenerateTicketToken creates an HMAC signed string for WebSocket authentication.
func GenerateTicketToken(userID, owner, repo, buildID string) string {
	expiresAt := time.Now().Add(60 * time.Second)
	ticket := fmt.Sprintf("%s|%s|%s|%s|%d", userID, owner, repo, buildID, expiresAt.Unix())
	mac := hmac.New(sha256.New, ticketSecret)
	mac.Write([]byte(ticket))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s|%s", ticket, sig)
}

// VerifyTicketToken checks the signature and expiration of a ticket token.
func VerifyTicketToken(tokenStr string) (*TicketClaims, error) {
	parts := strings.Split(tokenStr, "|")
	if len(parts) != 6 {
		return nil, errors.New("invalid token format")
	}

	userID := parts[0]
	owner := parts[1]
	repo := parts[2]
	buildID := parts[3]
	expiresUnix, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return nil, errors.New("invalid expiration in ticket")
	}
	sig := parts[5]

	ticket := strings.Join(parts[:5], "|")
	mac := hmac.New(sha256.New, ticketSecret)
	mac.Write([]byte(ticket))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return nil, errors.New("invalid signature")
	}

	expiresAt := time.Unix(expiresUnix, 0)
	if time.Now().After(expiresAt) {
		return nil, errors.New("ticket has expired")
	}

	return &TicketClaims{
		UserID:    userID,
		Owner:     owner,
		Repo:      repo,
		BuildID:   buildID,
		ExpiresAt: expiresAt,
	}, nil
}

// SignedURLCache caches generated GCS Signed URLs keylessly
type CachedURL struct {
	URL       string
	ExpiresAt time.Time
}

type SignedURLCache struct {
	mu    sync.RWMutex
	cache map[string]CachedURL
}

var urlCache = &SignedURLCache{
	cache: make(map[string]CachedURL),
}

func (c *SignedURLCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, found := c.cache[key]
	if !found {
		return "", false
	}
	// Verify that the cached URL is still valid and not expiring within the next 2 minutes
	if time.Now().Add(2 * time.Minute).After(entry.ExpiresAt) {
		return "", false
	}
	return entry.URL, true
}

func (c *SignedURLCache) Set(key string, url string, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = CachedURL{
		URL:       url,
		ExpiresAt: expiresAt,
	}
}

// getServiceAccountEmail determines the email to use for signing.
func getServiceAccountEmail() string {
	email := os.Getenv("CLOUD_BUILD_SERVICE_ACCOUNT")
	if email != "" {
		return email
	}
	if metadata.OnGCE() {
		if val, err := metadata.Email("default"); err == nil && val != "" {
			return val
		}
	}
	return ""
}

// GenerateKeylessSignedURLWithCache checks the cache before calling Google IAM Credentials API.
func GenerateKeylessSignedURLWithCache(ctx context.Context, bucket, object string, serviceAccountEmail string, expires time.Duration) (string, error) {
	cacheKey := bucket + "/" + object
	if cachedVal, found := urlCache.Get(cacheKey); found {
		return cachedVal, nil
	}

	if serviceAccountEmail == "" {
		if os.Getenv("DEV_MODE") == "true" {
			return fmt.Sprintf("http://localhost:9000/mock-signed-url/%s/%s", bucket, object), nil
		}
		return "", errors.New("service account email is required for signing URLs but could not be determined")
	}

	iamClient, err := credentials.NewIamCredentialsClient(ctx)
	if err != nil {
		return "", err
	}
	defer iamClient.Close()

	expiryTime := time.Now().Add(expires)
	opts := &storage.SignedURLOptions{
		Scheme:         storage.SigningSchemeV4,
		Method:         "GET",
		Expires:        expiryTime,
		GoogleAccessID: serviceAccountEmail,
		SignBytes: func(payload []byte) ([]byte, error) {
			req := &credentialspb.SignBlobRequest{
				Name:    "projects/-/serviceAccounts/" + serviceAccountEmail,
				Payload: payload,
			}
			resp, err := iamClient.SignBlob(ctx, req)
			if err != nil {
				return nil, err
			}
			return resp.SignedBlob, nil
		},
	}

	signedURL, err := storage.SignedURL(bucket, object, opts)
	if err != nil {
		return "", err
	}

	urlCache.Set(cacheKey, signedURL, expiryTime)
	return signedURL, nil
}

// VerifyCloudBuildOIDCToken validates the OIDC ID token sent by GCP Cloud Build/PubSub.
func VerifyCloudBuildOIDCToken(ctx context.Context, tokenString, audience string) (*idtoken.Payload, error) {
	payload, err := idtoken.Validate(ctx, tokenString, audience)
	if err != nil {
		return nil, fmt.Errorf("failed to validate OIDC token: %w", err)
	}

	projectNumber := os.Getenv("GOOGLE_PROJECT_NUMBER")
	if projectNumber != "" {
		emailVal, ok := payload.Claims["email"]
		if !ok {
			return nil, errors.New("missing email claim in OIDC token")
		}

		email, ok := emailVal.(string)
		if !ok {
			return nil, errors.New("invalid email claim format")
		}

		expectedEmail := fmt.Sprintf("service-%s@gcp-sa-cloudbuild.iam.gserviceaccount.com", projectNumber)
		if !strings.EqualFold(email, expectedEmail) {
			return nil, fmt.Errorf("unauthorized caller: email claim %q does not match expected service account %q", email, expectedEmail)
		}
	}

	return payload, nil
}

func readCloudBuildYaml(repoPath, sha string) ([]byte, error) {
	cmd := exec.Command("git", "--git-dir", repoPath, "show", fmt.Sprintf("%s:cloudbuild.yaml", sha))
	return cmd.Output()
}

// projectIDFromEnv resolves the GCP project ID using the same precedence as
// internal/config: PROJECT_ID first, then GOOGLE_CLOUD_PROJECT. Returns "" if
// neither is set (callers keep their own empty-string handling).
func projectIDFromEnv() string {
	if p := os.Getenv("PROJECT_ID"); p != "" {
		return p
	}
	return os.Getenv("GOOGLE_CLOUD_PROJECT")
}

func getGitbucketURL(r *http.Request) string {
	url := os.Getenv("GITBUCKET_URL")
	if url != "" {
		return strings.TrimSuffix(url, "/")
	}
	scheme := "https"
	if r.TLS == nil && os.Getenv("DEV_MODE") == "true" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// TriggerCloudBuild pulls cloudbuild.yaml from the commit and submits a build.
func (h *APIHandler) TriggerCloudBuild(owner, repo, branch, sha string) {
	ctx := context.Background()
	localRepoPath := filepath.Join(h.LocalReposRoot, strings.ToLower(owner), fmt.Sprintf("%s.git", strings.ToLower(repo)))

	// 1. Read cloudbuild.yaml from the repository
	yamlBytes, err := readCloudBuildYaml(localRepoPath, sha)
	if err != nil {
		log.Printf("[GCB Trigger] No cloudbuild.yaml or failed to read for %s/%s at %s: %v", owner, repo, sha, err)
		return
	}

	// 2. Parse YAML
	var parsed struct {
		Steps []struct {
			Name       string   `yaml:"name"`
			Entrypoint string   `yaml:"entrypoint"`
			Args       []string `yaml:"args"`
			Env        []string `yaml:"env"`
			Dir        string   `yaml:"dir"`
			ID         string   `yaml:"id"`
			WaitFor    []string `yaml:"waitFor"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(yamlBytes, &parsed); err != nil {
		log.Printf("[GCB Trigger] Failed to parse cloudbuild.yaml: %v", err)
		return
	}

	gitbucketURL := os.Getenv("GITBUCKET_URL")
	if gitbucketURL == "" {
		gitbucketURL = "https://gitbucket.dev"
	}
	gitbucketURL = strings.TrimSuffix(gitbucketURL, "/")

	// 3. Fallback/Simulate in DEV_MODE if no credentials/project is set
	projectID := projectIDFromEnv()
	if os.Getenv("DEV_MODE") == "true" && (projectID == "" || os.Getenv("MOCK_GCB") == "true") {
		log.Printf("[GCB Trigger] DEV_MODE enabled. Simulating Cloud Build dispatch...")
		buildID := "mock-build-" + sha[:8]
		statusID := fmt.Sprintf("%s_%s_%s_%s", owner, repo, sha, buildID)
		statusRef := h.FirestoreClient.Collection("commit_statuses").Doc(statusID)
		_, _ = statusRef.Set(ctx, map[string]interface{}{
			"id":          statusID,
			"owner":       owner,
			"repo":        repo,
			"sha":         sha,
			"buildId":     buildID,
			"status":      "QUEUED",
			"logBucket":   "mock-bucket",
			"logObject":   fmt.Sprintf("log-%s.txt", buildID),
			"externalUrl": "https://console.cloud.google.com/cloud-build/builds/" + buildID,
			"createdAt":   time.Now(),
			"updatedAt":   time.Now(),
		})
		return
	}

	if projectID == "" {
		log.Printf("[GCB Trigger] no project configured (set PROJECT_ID or GOOGLE_CLOUD_PROJECT)")
		return
	}

	// 4. Initialize Cloud Build client
	gcbService, err := cloudbuild.NewService(ctx)
	if err != nil {
		log.Printf("[GCB Trigger] Failed to create Cloud Build client: %v", err)
		return
	}

	// 5. Construct steps (first step is always the git clone step)
	cloneStep := &cloudbuild.BuildStep{
		Name:       "gcr.io/cloud-builders/git",
		Entrypoint: "bash",
		Args: []string{
			"-c",
			`ID_TOKEN=$(curl -sS -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity?audience=${_GITBUCKET_URL}/repos/${_REPO_OWNER}/${_REPO_NAME}")
if [ -z "$ID_TOKEN" ]; then
  echo "Error: Failed to fetch OIDC ID token from metadata server" >&2
  exit 1
fi
git config --global http.extraheader "Authorization: Bearer $ID_TOKEN"
git clone --depth 1 --branch "$_BRANCH_NAME" "${_GITBUCKET_URL}/r/${_REPO_OWNER}/${_REPO_NAME}" .`,
		},
	}

	steps := []*cloudbuild.BuildStep{cloneStep}
	for _, s := range parsed.Steps {
		steps = append(steps, &cloudbuild.BuildStep{
			Name:       s.Name,
			Entrypoint: s.Entrypoint,
			Args:       s.Args,
			Env:        s.Env,
			Dir:        s.Dir,
			Id:         s.ID,
			WaitFor:    s.WaitFor,
		})
	}

	build := &cloudbuild.Build{
		Steps: steps,
		Substitutions: map[string]string{
			"_COMMIT_SHA":    sha,
			"_BRANCH_NAME":   branch,
			"_REPO_OWNER":    owner,
			"_REPO_NAME":     repo,
			"_GITBUCKET_URL": gitbucketURL,
		},
	}

	serviceAccount := os.Getenv("CLOUD_BUILD_SERVICE_ACCOUNT")
	if serviceAccount != "" {
		build.ServiceAccount = serviceAccount
	}

	// 6. Submit Build
	op, err := gcbService.Projects.Builds.Create(projectID, build).Context(ctx).Do()
	if err != nil {
		log.Printf("[GCB Trigger] Failed to submit build: %v", err)
		return
	}

	// 7. Parse build ID from operation
	var opMeta struct {
		Build struct {
			ID         string `json:"id"`
			LogUrl     string `json:"logUrl"`
			LogsBucket string `json:"logsBucket"`
		} `json:"build"`
	}
	_ = json.Unmarshal(op.Metadata, &opMeta)
	buildID := opMeta.Build.ID
	if buildID == "" {
		log.Printf("[GCB Trigger] Failed to extract build ID from operation metadata: %s", string(op.Metadata))
		return
	}

	log.Printf("[GCB Trigger] Successfully dispatched Cloud Build ID %s for %s/%s at %s", buildID, owner, repo, sha)

	// 8. Write Commit Status to Firestore
	statusID := fmt.Sprintf("%s_%s_%s_%s", owner, repo, sha, buildID)
	statusRef := h.FirestoreClient.Collection("commit_statuses").Doc(statusID)
	_, err = statusRef.Set(ctx, map[string]interface{}{
		"id":          statusID,
		"owner":       owner,
		"repo":        repo,
		"sha":         sha,
		"buildId":     buildID,
		"status":      "QUEUED",
		"logBucket":   opMeta.Build.LogsBucket,
		"logObject":   fmt.Sprintf("log-%s.txt", buildID),
		"externalUrl": opMeta.Build.LogUrl,
		"createdAt":   time.Now(),
		"updatedAt":   time.Now(),
	})
	if err != nil {
		log.Printf("[GCB Trigger] Failed to save commit status: %v", err)
	}
}

// GetLogTicket handles POST /api/repos/{owner}/{repo}/builds/ticket
func (h *APIHandler) GetLogTicket(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	var req struct {
		BuildID string `json:"buildId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.BuildID == "" {
		http.Error(w, "Missing buildId", http.StatusBadRequest)
		return
	}

	// Authorize user has read access
	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("builds: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	uid := auth.GetUID(r.Context())
	meta := db.MapToRepositoryMetadata(repoMeta)
	if !auth.CanRead(meta, uid) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ticket := GenerateTicketToken(uid, owner, repo, req.BuildID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"ticket": ticket,
	})
}

// GetBuildLogsSignedURL handles GET /api/repos/{owner}/{repo}/builds/{buildId}/logs
func (h *APIHandler) GetBuildLogsSignedURL(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	buildID := chi.URLParam(r, "buildId")

	// 1. Authorize user (must have read access)
	repoMeta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		log.Printf("builds: GetRepositoryMetadata(%s/%s): %v", owner, repo, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if repoMeta == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	uid := auth.GetUID(r.Context())
	meta := db.MapToRepositoryMetadata(repoMeta)
	if !auth.CanRead(meta, uid) {
		http.Error(w, "Forbidden: read access required", http.StatusForbidden)
		return
	}

	// 2. Fetch commit status doc to get log bucket/object details
	query := h.FirestoreClient.Collection("commit_statuses").
		Where("owner", "==", owner).
		Where("repo", "==", repo).
		Where("buildId", "==", buildID).
		Limit(1)
	docs, err := query.Documents(r.Context()).GetAll()
	if err != nil {
		log.Printf("builds: commit_statuses query for %s/%s buildID=%s: %v", owner, repo, buildID, err)
		http.Error(w, "Database error querying status", http.StatusInternalServerError)
		return
	}
	if len(docs) == 0 {
		http.Error(w, "Build status not found", http.StatusNotFound)
		return
	}

	var data map[string]interface{}
	if err := docs[0].DataTo(&data); err != nil {
		log.Printf("builds: commit_statuses DataTo for %s/%s buildID=%s: %v", owner, repo, buildID, err)
		http.Error(w, "Data serialization error", http.StatusInternalServerError)
		return
	}

	logBucket, _ := data["logBucket"].(string)
	logObject, _ := data["logObject"].(string)

	if logBucket == "" {
		logBucket = h.BucketName
	}
	if logObject == "" {
		logObject = fmt.Sprintf("log-%s.txt", buildID)
	}

	// 3. Generate keyless signed URL
	serviceAccountEmail := getServiceAccountEmail()
	signedURL, err := GenerateKeylessSignedURLWithCache(r.Context(), logBucket, logObject, serviceAccountEmail, 2*time.Hour)
	if err != nil {
		if os.Getenv("DEV_MODE") == "true" {
			signedURL = fmt.Sprintf("http://localhost:9000/mock-signed-url/%s/%s", logBucket, logObject)
		} else {
			log.Printf("[GetLogs] Failed to generate signed URL: %v", err)
			http.Error(w, "Failed to generate log access link", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"signedUrl": signedURL,
	})
}

// HandleLiveLogsStream upgrading WebSocket connection and tails logs
func (h *APIHandler) HandleLiveLogsStream(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	buildID := chi.URLParam(r, "buildId")

	// 1. Verify Ticket Handshake
	ticketStr := r.URL.Query().Get("ticket")
	if ticketStr == "" {
		http.Error(w, "Unauthorized: missing ticket query parameter", http.StatusUnauthorized)
		return
	}

	claims, err := VerifyTicketToken(ticketStr)
	if err != nil {
		http.Error(w, "Unauthorized: invalid ticket: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Confirm the ticket matches this repository and build
	if !strings.EqualFold(claims.Owner, owner) || !strings.EqualFold(claims.Repo, repo) || claims.BuildID != buildID {
		http.Error(w, "Forbidden: ticket does not match target resource", http.StatusForbidden)
		return
	}

	// 2. Safe to upgrade the HTTP request to a WebSocket connection
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS Live Logs] Failed to upgrade: %v", err)
		return
	}
	defer ws.Close()

	ctx := r.Context()

	// If in DEV_MODE, let's stream mock logs for testing!
	if os.Getenv("DEV_MODE") == "true" {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lines := []string{
			"Starting build step 1...",
			"Running keyless OIDC clone...",
			"Cloned successfully!",
			"Starting build step 2...",
			"Running tests...",
			"Tests passed!",
			"Build successful!",
		}
		for i, line := range lines {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = ws.WriteMessage(websocket.TextMessage, []byte(line))
				if i == len(lines)-1 {
					_ = ws.WriteMessage(websocket.TextMessage, []byte("[EOF]"))
					return
				}
			}
		}
		return
	}

	projectID := projectIDFromEnv()
	if projectID == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("Error: no project configured (set PROJECT_ID or GOOGLE_CLOUD_PROJECT)"))
		return
	}

	// Real Google Cloud Logging tailing
	client, err := loggingapiv2.NewClient(ctx)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("Error: Failed to initialize Google Logging Client"))
		return
	}
	defer client.Close()

	tailClient, err := client.TailLogEntries(ctx)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("Error: Failed to open Cloud Logging Tail stream"))
		return
	}
	defer tailClient.CloseSend()

	filter := fmt.Sprintf(`resource.type="build" AND resource.labels.build_id=%q`, buildID)
	req := &loggingpb.TailLogEntriesRequest{
		ResourceNames: []string{fmt.Sprintf("projects/%s", projectID)},
		Filter:        filter,
	}

	if err := tailClient.Send(req); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("Error: Failed to send log filter request"))
		return
	}

	for {
		resp, err := tailClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		for _, entry := range resp.Entries {
			var logLine string
			if entry.GetTextPayload() != "" {
				logLine = entry.GetTextPayload()
			} else if entry.GetJsonPayload() != nil {
				logLine = fmt.Sprintf("%v", entry.GetJsonPayload())
			}

			if err := ws.WriteMessage(websocket.TextMessage, []byte(logLine)); err != nil {
				return
			}
		}
	}
}

// HandleCloudBuildWebhook processes the Pub/Sub pushes for Cloud Build status changes
func (h *APIHandler) HandleCloudBuildWebhook(w http.ResponseWriter, r *http.Request) {
	// If DEV_MODE is true, we can skip OIDC signature validation
	if os.Getenv("DEV_MODE") != "true" {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized: missing OIDC bearer token", http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		
		gitbucketURL := getGitbucketURL(r)
		audience := fmt.Sprintf("%s/api/webhooks/cloudbuild", gitbucketURL)
		
		_, err := VerifyCloudBuildOIDCToken(r.Context(), tokenStr, audience)
		if err != nil {
			log.Printf("[GCB Webhook] OIDC token validation failed: %v", err)
			http.Error(w, "Unauthorized: invalid OIDC token", http.StatusUnauthorized)
			return
		}
	}

	// Parse PubSub wrapper
	var wrapper struct {
		Message struct {
			Data []byte `json:"data"`
		} `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&wrapper); err != nil {
		log.Printf("[GCB Webhook] Failed to decode PubSub wrapper: %v", err)
		http.Error(w, "Invalid pubsub payload", http.StatusBadRequest)
		return
	}

	// Parse inner GCB build payload
	var payload struct {
		ID            string            `json:"id"`
		BuildID       string            `json:"buildId"`
		Status        string            `json:"status"`
		UpdateTime    string            `json:"updateTime"`
		Substitutions map[string]string `json:"substitutions"`
	}
	if err := json.Unmarshal(wrapper.Message.Data, &payload); err != nil {
		log.Printf("[GCB Webhook] Failed to decode inner payload: %v", err)
		http.Error(w, "Invalid GCB payload", http.StatusBadRequest)
		return
	}

	buildID := payload.BuildID
	if buildID == "" {
		buildID = payload.ID
	}

	if buildID == "" {
		http.Error(w, "Missing build ID in payload", http.StatusBadRequest)
		return
	}

	if payload.Substitutions == nil {
		http.Error(w, "Missing substitutions", http.StatusBadRequest)
		return
	}

	owner := payload.Substitutions["_REPO_OWNER"]
	repo := payload.Substitutions["_REPO_NAME"]
	sha := payload.Substitutions["_COMMIT_SHA"]

	if owner == "" || repo == "" || sha == "" {
		log.Printf("[GCB Webhook] Missing substitutions details: owner=%s, repo=%s, sha=%s", owner, repo, sha)
		http.Error(w, "Missing substitution fields", http.StatusBadRequest)
		return
	}

	// Verify repo exists to avoid orphaned commit status docs
	repoRef := h.FirestoreClient.Collection("repositories").Doc(strings.ToLower(owner) + "_" + strings.ToLower(repo))
	_, err := repoRef.Get(r.Context())
	if err != nil {
		if status.Code(err) == codes.NotFound {
			log.Printf("[GCB Webhook] Repository %s/%s not found. Discarding status update.", owner, repo)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Printf("[GCB Webhook] Error reading repo meta: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	updateTime, err := time.Parse(time.RFC3339, payload.UpdateTime)
	if err != nil {
		updateTime = time.Now()
	}

	statusID := fmt.Sprintf("%s_%s_%s_%s", owner, repo, sha, buildID)
	statusRef := h.FirestoreClient.Collection("commit_statuses").Doc(statusID)

	err = h.FirestoreClient.RunTransaction(r.Context(), func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(statusRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				// Record doesn't exist, safe to create
				newStatus := map[string]interface{}{
					"id":           statusID,
					"status":       payload.Status,
					"owner":        owner,
					"repo":         repo,
					"sha":          sha,
					"buildId":      buildID,
					"gcbUpdatedAt": updateTime,
					"createdAt":    time.Now(),
					"updatedAt":    time.Now(),
				}
				return tx.Set(statusRef, newStatus)
			}
			return err
		}

		var currentData map[string]interface{}
		if err := doc.DataTo(&currentData); err != nil {
			return err
		}

		// 1. Timestamp check: Discard out-of-order late-arriving messages
		if val, ok := currentData["gcbUpdatedAt"]; ok {
			var currentGcbUpdate time.Time
			switch v := val.(type) {
			case time.Time:
				currentGcbUpdate = v
			case string:
				currentGcbUpdate, _ = time.Parse(time.RFC3339, v)
			}
			if !updateTime.After(currentGcbUpdate) {
				log.Printf("[GCB Webhook] Webhook timestamp %v is not newer than current %v. Discarding.", updateTime, currentGcbUpdate)
				return nil
			}
		}

		// 2. State transition guard: Block terminal -> active transitions
		if val, ok := currentData["status"]; ok {
			currentStatus := val.(string)
			if IsTerminalState(currentStatus) && !IsTerminalState(payload.Status) {
				log.Printf("[GCB Webhook] Blocked invalid state transition from %s back to %s. Discarding.", currentStatus, payload.Status)
				return nil
			}
		}

		// 3. Apply updates
		updates := []firestore.Update{
			{Path: "status", Value: payload.Status},
			{Path: "gcbUpdatedAt", Value: updateTime},
			{Path: "updatedAt", Value: time.Now()},
		}
		return tx.Update(statusRef, updates)
	})

	if err != nil {
		log.Printf("[GCB Webhook] Transaction failed: %v", err)
		http.Error(w, "Transaction failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "updated"}`))
}

func IsTerminalState(state string) bool {
	switch state {
	case "SUCCESS", "FAILURE", "TIMEOUT", "CANCELLED":
		return true
	default:
		return false
	}
}
