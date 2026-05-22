package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gitbucket/internal/apps"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WebhookCommit represents a commit object within the GitHub push webhook payload.
type WebhookCommit struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Author    struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Username string `json:"username"`
	} `json:"author"`
	Committer struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Username string `json:"username"`
	} `json:"committer"`
}

// GitHubPushPayload represents the GitHub webhook payload for a push event.
type GitHubPushPayload struct {
	Ref    string `json:"ref"`
	Before string `json:"before"`
	After  string `json:"after"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"owner"`
	} `json:"repository"`
	HeadCommit *WebhookCommit  `json:"head_commit"`
	Commits    []WebhookCommit `json:"commits"`
}

// knownSyncIdentities was a hardcoded map; bot identity is now sourced from
// apps.IsBotIdentity which reads from a Firestore-backed cache seeded with
// the same baseline names. See internal/apps/bot_identity.go.

// VerifyGitHubSignature verifies the HMAC-SHA256 signature from GitHub webhook.
func VerifyGitHubSignature(r *http.Request, secret []byte) bool {
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		return false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body)) // restore reader

	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sigHex := signature[len("sha256="):]
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expectedMAC := mac.Sum(nil)

	return hmac.Equal(sigBytes, expectedMAC)
}

// HandleGitHubWebhook handles incoming GitHub webhook POST requests.
func (h *APIHandler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret != "" {
		if !VerifyGitHubSignature(r, []byte(secret)) {
			log.Printf("[Webhook] GitHub signature verification failed")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	} else {
		log.Printf("[Webhook] Warning: GITHUB_WEBHOOK_SECRET is not set. Skipping verification.")
	}

	var payload GitHubPushPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	owner := payload.Repository.Owner.Login
	if owner == "" {
		owner = payload.Repository.Owner.Name
	}
	repo := payload.Repository.Name

	if owner == "" || repo == "" {
		http.Error(w, "Missing repository details", http.StatusBadRequest)
		return
	}

	// 1. Loop prevention check against sender and pusher
	if apps.IsBotIdentity(r.Context(), payload.Sender.Login) ||
		apps.IsBotIdentity(r.Context(), payload.Pusher.Name) {
		log.Printf("[Webhook] Discarding webhook due to sync bot sender/pusher identity: %s/%s", payload.Sender.Login, payload.Pusher.Name)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 2. Loop prevention check against head commit
	if payload.HeadCommit != nil {
		if apps.IsBotIdentity(r.Context(), payload.HeadCommit.Author.Username) ||
			apps.IsBotIdentity(r.Context(), payload.HeadCommit.Committer.Username) {
			log.Printf("[Webhook] Discarding webhook due to sync bot head commit author/committer identity")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.Contains(payload.HeadCommit.Message, "Synced-By: GitBucket") {
			log.Printf("[Webhook] Discarding webhook carrying Synced-By: GitBucket trailer in head commit")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// 3. Loop prevention check against all commits
	for _, commit := range payload.Commits {
		if apps.IsBotIdentity(r.Context(), commit.Author.Username) ||
			apps.IsBotIdentity(r.Context(), commit.Committer.Username) {
			log.Printf("[Webhook] Discarding webhook due to sync bot commit author/committer identity: %s/%s", commit.Author.Username, commit.Committer.Username)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.Contains(commit.Message, "Synced-By: GitBucket") {
			log.Printf("[Webhook] Discarding webhook carrying Synced-By: GitBucket trailer in commits")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// 4. Loop prevention: Check recent pushed SHAs in Firestore (recent_sync_pushes/{sha})
	if payload.After != "" && payload.After != "0000000000000000000000000000000000000000" {
		shaDocRef := h.FirestoreClient.Collection("recent_sync_pushes").Doc(payload.After)
		doc, err := shaDocRef.Get(r.Context())
		if err == nil && doc.Exists() {
			var data map[string]interface{}
			if err := doc.DataTo(&data); err == nil {
				if createdAtVal, ok := data["createdAt"]; ok {
					if createdAt, ok := createdAtVal.(time.Time); ok {
						if time.Since(createdAt) < 5*time.Minute {
							log.Printf("[Webhook] Loop prevention: Discarding webhook matching recently pushed SHA: %s", payload.After)
							w.WriteHeader(http.StatusNoContent)
							return
						}
					}
				}
			}
		} else if err != nil && status.Code(err) != codes.NotFound {
			log.Printf("[Webhook] Error querying recent_sync_pushes: %v", err)
		}
	}

	log.Printf("[Webhook] Received valid push event for %s/%s (Ref: %s, After: %s). Processing sync...", owner, repo, payload.Ref, payload.After)

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status": "accepted"}`))
}
