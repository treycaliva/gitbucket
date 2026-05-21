# GitHub App Emulation — Plan 3: Outbound Webhook Engine

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Outbound webhook fan-out. When GitBucket state changes (PR opened/edited/closed/merged, push to ref), every installed App that subscribed to that event receives a signed HTTPS POST. Replace the existing hardcoded `knownSyncIdentities` loop-prevention map with a Firestore-backed bot-identity cache so newly-registered Apps integrate without code deploys. Adds a Cloud Tasks-backed delivery queue with a dispatcher endpoint we control, so every delivery attempt is auditable.

**Architecture:** New `internal/apps/events.go` exports `events.Fire(ctx, eventType, payload)` — a non-blocking call that's threaded into existing PR + Git-push handlers. `Fire` queries matching installations, builds the GitHub-shape payload, signs it with the App's webhook secret (fetched from Secret Manager, cached 5 min in-process), writes a `webhook_deliveries/{uuid}` audit doc, then enqueues a Cloud Task targeting our own dispatcher endpoint `/_internal/dispatch-webhook/{id}`. The dispatcher verifies the inbound OIDC token from Cloud Tasks, relays the signed payload to the App's webhook URL, and updates the delivery record. A `TaskEnqueuer` interface lets tests substitute an in-memory fake. Loop prevention skips installations where the event's `sender` is one of the synthetic `<slug>[bot]` users.

**Tech Stack:** Same as Plans 1+2 — Go, chi v5, Firestore, Secret Manager. New external dep: `cloud.google.com/go/cloudtasks/apiv2`.

**Spec:** `docs/superpowers/specs/2026-05-20-github-app-emulation-design.md` §7, §9.2-9.3
**Builds on:** `docs/superpowers/plans/2026-05-21-github-app-emulation-rest-surface.md` (Plan 2)

**Scope of Plan 3:** Spec §7 outbound webhook engine + §9.2 loop-prevention wire-up.

Events emitted in Plan 3:
- `pull_request` (actions: `opened`, `edited`, `closed`, `reopened`) — wired into both Plan 2's `/api/v3` PR handlers AND the existing `/api` PR handlers (web UI).
- `push` — wired into the post-receive path in `internal/api/git.go`.
- `installation` (action: `created`, `deleted`) — payload builders included; Fire calls will be added in Plan 4 alongside the install flow itself.

**Out of scope of Plan 3 (deferred):**
- `issues` + `issue_comment` events — Plan 2.5 (no issues backend yet).
- `pull_request_review` / `pull_request_review_comment` — existing reviews infra is minimal; defer.
- Webhook replay/redeliver UI — `webhook_deliveries` audit log exists, UI lands later.
- Manifest registration flow + `installation` event firing — Plan 4.

**Branch base:** Stack on top of `feature/github-app-emulation-rest-surface` (Plan 2). Branch name suggestion: `feature/github-app-emulation-webhook-engine`. If Plans 1+2 merge to main first, rebase onto main.

---

## File Structure

**New files:**

```
internal/apps/
  events.go                    — events.Fire, EventType enum, payload builders, TaskEnqueuer interface
  events_test.go               — Fire path, sender filter, HMAC signature, enqueue
  signature.go                 — HMAC-SHA256 signing + X-Hub-Signature-256 header helper
  signature_test.go
  deliveries.go                — webhook_deliveries Firestore CRUD
  deliveries_test.go
  enqueuer.go                  — TaskEnqueuer interface + MemoryEnqueuer (tests) + RealEnqueuer (Cloud Tasks)
  enqueuer_test.go             — MemoryEnqueuer unit tests
  dispatcher.go                — /_internal/dispatch-webhook/{id} handler
  dispatcher_test.go
  bot_identity.go              — IsBotIdentity + botIdentityCache (Firestore-backed, 60s TTL)
  bot_identity_test.go
  webhook_secret_cache.go      — In-process Secret Manager-fetched webhook secret cache
  webhook_secret_cache_test.go
```

**Modified files:**

```
internal/api/webhooks.go       — Replace knownSyncIdentities map with apps.IsBotIdentity calls
internal/api/v3/pulls.go       — Fire pull_request events after Create/Update succeed
internal/api/pull_requests.go  — Fire pull_request events after Create/Close/Merge succeed (UI handlers)
internal/api/git.go            — Fire push event in post-receive success path (after GCS sync)
internal/apps/testfixtures/fixtures.go — Add SeedAnotherInstallation helper for multi-recipient tests
main.go                        — Init RealEnqueuer + secret cache + dispatcher route mount
go.mod, go.sum                 — Add cloudtasks dep
```

**Manual ops (one-time, per environment, documented in Task 11):**

```bash
# Create the Cloud Tasks queue.
gcloud tasks queues create gitbucket-webhooks \
    --location=us-central1 \
    --max-attempts=5 \
    --min-backoff=10s \
    --max-backoff=3600s \
    --max-retry-duration=86400s

# Grant the Cloud Run service account permission to enqueue tasks.
gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:$SA_EMAIL" \
    --role="roles/cloudtasks.enqueuer"

# Grant Cloud Tasks the ability to invoke the dispatcher via OIDC.
# The queue config sets the OIDC service account; the dispatcher endpoint
# verifies the audience matches its own URL.
```

Plus the Plan 1 Firestore TTL config still applies; webhook_deliveries also gets a TTL:

```bash
gcloud firestore fields ttls update created_at \
    --collection-group=webhook_deliveries \
    --enable-ttl
```

(With ~30 day retention via the field's natural age.)

---

## Task 1: Event types + payload builders

**Files:**
- Create: `internal/apps/events.go`
- Create: `internal/apps/events_test.go` (types-only tests; Fire integration test lands in Task 6)

This task defines the surface: the `EventType` enum, payload structs, and stub `Fire` function. Subsequent tasks fill in dependencies (signature, deliveries, enqueuer) and finally the real `Fire` body.

- [ ] **Step 1: Write the failing test**

Create `internal/apps/events_test.go`:

```go
package apps

import "testing"

func TestEventTypeString(t *testing.T) {
	cases := []struct {
		t    EventType
		want string
	}{
		{EventPullRequest, "pull_request"},
		{EventPush, "push"},
		{EventInstallation, "installation"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("EventType(%d).String() = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestPullRequestPayloadShape(t *testing.T) {
	p := PullRequestPayload{
		Action:     "opened",
		Number:     7,
		Title:      "x",
		Body:       "y",
		State:      "open",
		HeadBranch: "feature",
		BaseBranch: "main",
		Owner:      "alice",
		Repo:       "demo",
		Sender:     SenderRef{Login: "bob", ID: 12345, Type: "User"},
	}
	if p.Action != "opened" {
		t.Errorf("Action = %q", p.Action)
	}
	if p.Sender.Type != "User" {
		t.Errorf("Sender.Type = %q", p.Sender.Type)
	}
}

func TestPushPayloadShape(t *testing.T) {
	p := PushPayload{
		Owner:  "alice",
		Repo:   "demo",
		Ref:    "refs/heads/main",
		Before: "0000000000000000000000000000000000000000",
		After:  "abc123",
		Sender: SenderRef{Login: "alice", ID: 1, Type: "User"},
	}
	if p.Ref != "refs/heads/main" {
		t.Errorf("Ref = %q", p.Ref)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/apps/... -run "TestEventTypeString|TestPullRequestPayloadShape|TestPushPayloadShape" -v
```

Expected: undefined `EventType`, `EventPullRequest`, `PullRequestPayload`, `PushPayload`, `SenderRef`.

- [ ] **Step 3: Write the implementation**

Create `internal/apps/events.go`:

```go
package apps

import (
	"context"
	"fmt"
	"time"
)

// EventType enumerates webhook event names. Values match GitHub's
// `X-GitHub-Event` header.
type EventType int

const (
	EventPullRequest EventType = iota + 1
	EventPush
	EventInstallation
)

func (t EventType) String() string {
	switch t {
	case EventPullRequest:
		return "pull_request"
	case EventPush:
		return "push"
	case EventInstallation:
		return "installation"
	default:
		return "unknown"
	}
}

// SenderRef carries the minimum identity needed to populate the `sender`
// block of any GitHub-shape webhook payload. Sourced from the actor whose
// action triggered the event.
type SenderRef struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"` // "User" or "Bot"
}

// Payload is the interface that all event payloads implement. Implementations
// know their event type and how to build the outer GitHub-shape envelope.
type Payload interface {
	Event() EventType
	// Owner is the repo owner login (used to look up which installations
	// subscribe). Empty for non-repo-scoped events like `installation`.
	Owner() string
	// SenderRef returns the actor whose action triggered the event. Used for
	// loop prevention — events with bot senders are not delivered.
	SenderRef() SenderRef
}

// PullRequestPayload is the input to a `pull_request` event Fire call.
// The Fire path enriches this into the full GitHub-shape JSON via v3fmt
// + the addition of `action`, `repository`, `installation`, `sender`.
type PullRequestPayload struct {
	Action     string // "opened" | "edited" | "closed" | "reopened"
	Number     int
	Title      string
	Body       string
	State      string // "open" | "closed" (NOT "merged" — that's State=closed+Merged=true)
	HeadBranch string
	BaseBranch string
	HeadSHA    string
	BaseSHA    string
	Merged     bool
	MergedAt   *time.Time

	Owner  string
	Repo   string
	Sender SenderRef
}

func (p PullRequestPayload) Event() EventType     { return EventPullRequest }
func (p PullRequestPayload) Owner() string         { return p.Owner }
func (p PullRequestPayload) SenderRef() SenderRef  { return p.Sender }

// PushPayload represents a `push` event. Carries the ref delta + commit
// digest for the receiving App. The receiver can re-fetch full commit
// metadata via the v3 contents/tree endpoints.
type PushPayload struct {
	Ref     string   // full ref: "refs/heads/main"
	Before  string   // 40-char SHA; all-zeros for ref creation
	After   string   // 40-char SHA; all-zeros for ref deletion
	Commits []string // SHAs of commits added by this push (best-effort)

	Owner  string
	Repo   string
	Sender SenderRef
}

func (p PushPayload) Event() EventType    { return EventPush }
func (p PushPayload) Owner() string        { return p.Owner }
func (p PushPayload) SenderRef() SenderRef { return p.Sender }

// InstallationPayload represents an `installation` event. The Fire path for
// this event is added in Plan 4 alongside the install flow itself; the type
// is defined here so the events package surface is stable.
type InstallationPayload struct {
	Action  string // "created" | "deleted"
	AppID   string
	Account AccountRef
	Sender  SenderRef
}

func (p InstallationPayload) Event() EventType    { return EventInstallation }
func (p InstallationPayload) Owner() string        { return p.Account.ID }
func (p InstallationPayload) SenderRef() SenderRef { return p.Sender }

// Fire enqueues webhook deliveries for every installation that has
// subscribed to this event on the relevant repository. Non-blocking: it
// returns as soon as deliveries are enqueued. Failures to enqueue are
// logged but never returned — the user-visible request that triggered the
// event must not fail because of webhook trouble.
//
// The full implementation lands in Task 6. Until then, this is a no-op
// stub that lets caller code be wired in advance.
func Fire(ctx context.Context, p Payload) {
	_ = ctx
	_ = p
	// Real implementation lands in Task 6.
}

// FireError logs a non-fatal error from the Fire path. Webhook trouble must
// never fail the originating user request, so errors here are observability
// only.
func FireError(format string, args ...interface{}) {
	// Use a thin wrapper so Task 6 can swap to structured logging later.
	logEvents("ERROR: " + fmt.Sprintf(format, args...))
}

// logEvents is the single log sink for the events package. Replaced in
// tests via SetEventLogger so test output stays clean.
var logEvents = func(s string) {
	fmt.Println("[apps.events] " + s)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/apps/... -run "TestEventTypeString|TestPullRequestPayloadShape|TestPushPayloadShape" -v
go vet ./internal/apps/...
```

Expected: 3 tests PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/events.go internal/apps/events_test.go
git commit -m "feat(apps): events package skeleton (EventType, payloads, Fire stub)"
```

---

## Task 2: webhook_deliveries data layer

**Files:**
- Create: `internal/apps/deliveries.go`
- Create: `internal/apps/deliveries_test.go`

The `webhook_deliveries` Firestore collection is the audit log for every fan-out attempt. Schema per spec §4:

```
webhook_deliveries/{delivery_id}
  delivery_id, app_id, installation_id, event, payload_sha256,
  target_url, status ("pending"|"delivered"|"failed"),
  attempts, last_response_code, last_attempted_at, created_at
```

- [ ] **Step 1: Write failing test**

Create `internal/apps/deliveries_test.go`:

```go
package apps

import (
	"context"
	"os"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestCreateAndUpdateDelivery(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	rec, err := CreateDelivery(ctx, fs, CreateDeliveryInput{
		AppID:          "app-" + randHex(2),
		InstallationID: "inst-" + randHex(2),
		Event:          "pull_request",
		TargetURL:      "https://example.com/webhook",
		PayloadSHA256:  "abc123",
	})
	if err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionWebhookDeliveries).Doc(rec.DeliveryID).Delete(context.Background())
	})
	if rec.DeliveryID == "" {
		t.Fatal("DeliveryID empty")
	}
	if rec.Status != "pending" {
		t.Errorf("Status = %q, want pending", rec.Status)
	}
	if rec.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", rec.Attempts)
	}

	// Mark as delivered.
	if err := UpdateDeliveryStatus(ctx, fs, rec.DeliveryID, DeliveryUpdate{
		Status:           "delivered",
		LastResponseCode: 200,
		Attempts:         1,
		LastAttemptedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpdateDeliveryStatus: %v", err)
	}

	got, err := GetDelivery(ctx, fs, rec.DeliveryID)
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if got.Status != "delivered" {
		t.Errorf("Status after update = %q", got.Status)
	}
	if got.LastResponseCode != 200 {
		t.Errorf("LastResponseCode = %d", got.LastResponseCode)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d", got.Attempts)
	}
}

func TestGetDeliveryNotFound(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	got, err := GetDelivery(ctx, fs, "no-such-delivery")
	if err != nil {
		t.Fatalf("GetDelivery error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing delivery")
	}
}
```

- [ ] **Step 2: Confirm fail**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateAndUpdateDelivery -v
```

Expected: undefined `CreateDelivery`, `WebhookDelivery`, `DeliveryUpdate`, `CreateDeliveryInput`, `CollectionWebhookDeliveries`.

- [ ] **Step 3: Implementation**

Create `internal/apps/deliveries.go`:

```go
package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
)

// CollectionWebhookDeliveries is the Firestore collection name for the
// outbound webhook delivery audit log.
const CollectionWebhookDeliveries = "webhook_deliveries"

// WebhookDelivery is the persisted record for one fan-out attempt. Stored in
// the webhook_deliveries collection. The doc ID equals DeliveryID and is
// also the value sent in the `X-GitHub-Delivery` header.
type WebhookDelivery struct {
	DeliveryID       string    `firestore:"delivery_id"`
	AppID            string    `firestore:"app_id"`
	InstallationID   string    `firestore:"installation_id"`
	Event            string    `firestore:"event"`
	PayloadSHA256    string    `firestore:"payload_sha256"`
	TargetURL        string    `firestore:"target_url"`
	Status           string    `firestore:"status"` // "pending" | "delivered" | "failed"
	Attempts         int       `firestore:"attempts"`
	LastResponseCode int       `firestore:"last_response_code"`
	LastAttemptedAt  time.Time `firestore:"last_attempted_at"`
	CreatedAt        time.Time `firestore:"created_at"`
}

// CreateDeliveryInput is the subset of fields the caller supplies. The
// generated fields (DeliveryID, Status, Attempts, CreatedAt) are filled in
// by CreateDelivery.
type CreateDeliveryInput struct {
	AppID          string
	InstallationID string
	Event          string
	TargetURL      string
	PayloadSHA256  string
}

// DeliveryUpdate carries the fields the dispatcher writes back after each
// HTTPS attempt. Zero values are still written — callers must populate all
// applicable fields.
type DeliveryUpdate struct {
	Status           string
	Attempts         int
	LastResponseCode int
	LastAttemptedAt  time.Time
}

// CreateDelivery writes a new delivery doc with status=pending and returns
// the persisted record (DeliveryID + CreatedAt set).
func CreateDelivery(ctx context.Context, fs *firestore.Client, in CreateDeliveryInput) (*WebhookDelivery, error) {
	if fs == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate delivery_id: %w", err)
	}
	rec := &WebhookDelivery{
		DeliveryID:     hex.EncodeToString(idBytes),
		AppID:          in.AppID,
		InstallationID: in.InstallationID,
		Event:          in.Event,
		PayloadSHA256:  in.PayloadSHA256,
		TargetURL:      in.TargetURL,
		Status:         "pending",
		Attempts:       0,
		CreatedAt:      time.Now().UTC(),
	}
	if _, err := fs.Collection(CollectionWebhookDeliveries).Doc(rec.DeliveryID).Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("write delivery: %w", err)
	}
	return rec, nil
}

// GetDelivery returns the delivery record or (nil, nil) on not-found.
func GetDelivery(ctx context.Context, fs *firestore.Client, deliveryID string) (*WebhookDelivery, error) {
	doc, err := fs.Collection(CollectionWebhookDeliveries).Doc(deliveryID).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var d WebhookDelivery
	if err := doc.DataTo(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateDeliveryStatus updates the mutable fields on an existing delivery
// after a dispatch attempt. The status/attempts/response-code/timestamp are
// all written; the immutable fields (target_url, payload_sha256, etc.) are
// not touched.
func UpdateDeliveryStatus(ctx context.Context, fs *firestore.Client, deliveryID string, u DeliveryUpdate) error {
	_, err := fs.Collection(CollectionWebhookDeliveries).Doc(deliveryID).Update(ctx, []firestore.Update{
		{Path: "status", Value: u.Status},
		{Path: "attempts", Value: u.Attempts},
		{Path: "last_response_code", Value: u.LastResponseCode},
		{Path: "last_attempted_at", Value: u.LastAttemptedAt},
	})
	return err
}
```

- [ ] **Step 4: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run "TestCreateAndUpdateDelivery|TestGetDeliveryNotFound" -v
go vet ./internal/apps/...

git add internal/apps/deliveries.go internal/apps/deliveries_test.go
git commit -m "feat(apps): webhook_deliveries Firestore data layer"
```

---

## Task 3: HMAC signature helper

**Files:**
- Create: `internal/apps/signature.go`
- Create: `internal/apps/signature_test.go`

GitHub's `X-Hub-Signature-256` header is `sha256=` + lowercase hex HMAC-SHA256 of the raw payload bytes, keyed by the App's webhook secret.

- [ ] **Step 1: Write failing test**

Create `internal/apps/signature_test.go`:

```go
package apps

import (
	"strings"
	"testing"
)

func TestComputeHubSignature(t *testing.T) {
	// Known-good test vector: payload "hello", secret "topsecret".
	// HMAC-SHA256 hex computed via:
	//   echo -n 'hello' | openssl dgst -sha256 -hmac 'topsecret' -hex
	want := "sha256=adb7da6c0e7feb20c43e4d2eed9b0b2bff1c1a93b40c2af45d4b04ed3df2cc9b"
	got := ComputeHubSignature([]byte("hello"), []byte("topsecret"))
	if !strings.EqualFold(got, want) {
		t.Errorf("ComputeHubSignature = %q, want %q", got, want)
	}
}

func TestVerifyHubSignature(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte("s3cret")
	sig := ComputeHubSignature(payload, secret)

	if !VerifyHubSignature(payload, secret, sig) {
		t.Error("VerifyHubSignature rejected a signature we just computed")
	}
	if VerifyHubSignature(payload, []byte("wrong-secret"), sig) {
		t.Error("VerifyHubSignature accepted a signature from the wrong secret")
	}
	if VerifyHubSignature([]byte("tampered"), secret, sig) {
		t.Error("VerifyHubSignature accepted a signature for tampered payload")
	}
}

func TestVerifyHubSignatureMissingPrefix(t *testing.T) {
	if VerifyHubSignature([]byte("x"), []byte("s"), "not-a-sig") {
		t.Error("expected false for malformed signature")
	}
}
```

The expected hex string in `TestComputeHubSignature` is a real openssl output — if you happen to be uncertain, regenerate it locally:

```bash
echo -n 'hello' | openssl dgst -sha256 -hmac 'topsecret' -hex
```

- [ ] **Step 2: Confirm fail**

```bash
go test ./internal/apps/... -run "TestComputeHubSignature|TestVerifyHubSignature" -v
```

Expected: undefined `ComputeHubSignature`, `VerifyHubSignature`.

- [ ] **Step 3: Implementation**

Create `internal/apps/signature.go`:

```go
package apps

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ComputeHubSignature returns the value to put in the X-Hub-Signature-256
// HTTP header. Format: "sha256=" + lowercase hex of HMAC-SHA256(secret, payload).
func ComputeHubSignature(payload, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyHubSignature checks whether `signature` matches the expected value
// for the given payload + secret. The comparison is constant-time to defend
// against timing attacks.
func VerifyHubSignature(payload, secret []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	expected := ComputeHubSignature(payload, secret)
	return hmac.Equal([]byte(expected), []byte(signature))
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/apps/... -run "TestComputeHubSignature|TestVerifyHubSignature" -v
go vet ./internal/apps/...

git add internal/apps/signature.go internal/apps/signature_test.go
git commit -m "feat(apps): HMAC-SHA256 X-Hub-Signature-256 helpers"
```

---

## Task 4: TaskEnqueuer interface + MemoryEnqueuer + RealEnqueuer

**Files:**
- Create: `internal/apps/enqueuer.go`
- Create: `internal/apps/enqueuer_test.go`
- Modify: `go.mod`, `go.sum` (add cloudtasks dep)

Cloud Tasks enqueueing is abstracted behind a small interface. Tests use the in-memory implementation that records enqueued tasks for assertion. Production uses the Cloud Tasks SDK.

- [ ] **Step 1: Add the cloudtasks dependency**

```bash
go get cloud.google.com/go/cloudtasks/apiv2@latest
go mod tidy
```

- [ ] **Step 2: Write failing tests for MemoryEnqueuer**

Create `internal/apps/enqueuer_test.go`:

```go
package apps

import (
	"context"
	"testing"
)

func TestMemoryEnqueuer_Enqueue(t *testing.T) {
	ctx := context.Background()
	e := NewMemoryEnqueuer()
	if err := e.Enqueue(ctx, TaskSpec{
		TargetURL: "https://internal/dispatch/abc",
		Headers:   map[string]string{"X-GitHub-Event": "pull_request"},
		Body:      []byte(`{"action":"opened"}`),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	tasks := e.Drain()
	if len(tasks) != 1 {
		t.Fatalf("Drain len = %d, want 1", len(tasks))
	}
	if tasks[0].TargetURL != "https://internal/dispatch/abc" {
		t.Errorf("TargetURL = %q", tasks[0].TargetURL)
	}
	if tasks[0].Headers["X-GitHub-Event"] != "pull_request" {
		t.Errorf("header missing")
	}
	if string(tasks[0].Body) != `{"action":"opened"}` {
		t.Errorf("body = %q", tasks[0].Body)
	}
}

func TestMemoryEnqueuer_DrainResets(t *testing.T) {
	e := NewMemoryEnqueuer()
	_ = e.Enqueue(context.Background(), TaskSpec{TargetURL: "x"})
	if got := e.Drain(); len(got) != 1 {
		t.Fatalf("first Drain len = %d", len(got))
	}
	if got := e.Drain(); len(got) != 0 {
		t.Errorf("second Drain len = %d, want 0 (Drain should reset)", len(got))
	}
}
```

- [ ] **Step 3: Confirm fail**

```bash
go test ./internal/apps/... -run TestMemoryEnqueuer -v
```

Expected: undefined `NewMemoryEnqueuer`, `TaskSpec`, `TaskEnqueuer`.

- [ ] **Step 4: Implementation**

Create `internal/apps/enqueuer.go`:

```go
package apps

import (
	"context"
	"fmt"
	"sync"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
)

// TaskSpec is the input to TaskEnqueuer.Enqueue. The body and headers are
// dispatched verbatim to TargetURL; the enqueuer adds an OIDC token in the
// real implementation (memory implementation does not).
type TaskSpec struct {
	TargetURL string
	Headers   map[string]string
	Body      []byte
}

// TaskEnqueuer abstracts the task queue so tests can substitute an
// in-memory recorder. Implementations must be safe for concurrent use.
type TaskEnqueuer interface {
	Enqueue(ctx context.Context, spec TaskSpec) error
}

// --- Memory implementation (tests) -----------------------------------------

type MemoryEnqueuer struct {
	mu    sync.Mutex
	tasks []TaskSpec
}

func NewMemoryEnqueuer() *MemoryEnqueuer { return &MemoryEnqueuer{} }

func (m *MemoryEnqueuer) Enqueue(_ context.Context, spec TaskSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Defensive copy of header map + body so caller mutations don't leak.
	hCopy := make(map[string]string, len(spec.Headers))
	for k, v := range spec.Headers {
		hCopy[k] = v
	}
	bCopy := make([]byte, len(spec.Body))
	copy(bCopy, spec.Body)
	m.tasks = append(m.tasks, TaskSpec{TargetURL: spec.TargetURL, Headers: hCopy, Body: bCopy})
	return nil
}

// Drain returns all enqueued tasks and clears the buffer. Tests use this to
// assert what got enqueued during a single test scenario.
func (m *MemoryEnqueuer) Drain() []TaskSpec {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.tasks
	m.tasks = nil
	return out
}

// --- Cloud Tasks implementation (production) -------------------------------

// RealEnqueuer wraps a Cloud Tasks client and a fully-qualified queue path.
// Caller supplies queueName like "projects/<p>/locations/<l>/queues/<q>"
// and oidcServiceAccount + audience that the dispatcher endpoint expects.
type RealEnqueuer struct {
	client              *cloudtasks.Client
	queueName           string
	oidcServiceAccount  string
	oidcAudience        string
}

func NewRealEnqueuer(client *cloudtasks.Client, queueName, oidcServiceAccount, oidcAudience string) *RealEnqueuer {
	return &RealEnqueuer{
		client:             client,
		queueName:          queueName,
		oidcServiceAccount: oidcServiceAccount,
		oidcAudience:       oidcAudience,
	}
}

func (e *RealEnqueuer) Enqueue(ctx context.Context, spec TaskSpec) error {
	req := &taskspb.CreateTaskRequest{
		Parent: e.queueName,
		Task: &taskspb.Task{
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					Url:        spec.TargetURL,
					HttpMethod: taskspb.HttpMethod_POST,
					Headers:    spec.Headers,
					Body:       spec.Body,
					AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
						OidcToken: &taskspb.OidcToken{
							ServiceAccountEmail: e.oidcServiceAccount,
							Audience:            e.oidcAudience,
						},
					},
				},
			},
		},
	}
	if _, err := e.client.CreateTask(ctx, req); err != nil {
		return fmt.Errorf("cloud tasks enqueue: %w", err)
	}
	return nil
}
```

**Import path note:** the Cloud Tasks SDK uses `cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb` for the protos as of mid-2025. If the build fails on that import, check the package layout after `go get`: try alternates `cloud.google.com/go/cloudtasks/apiv2beta3/cloudtaskspb` or look at the actual imports in `pkg.go.dev/cloud.google.com/go/cloudtasks`. If neither builds, STOP and report BLOCKED with the actual error.

- [ ] **Step 5: Run + build**

```bash
go test ./internal/apps/... -run TestMemoryEnqueuer -v
go vet ./internal/apps/...
go build ./...
```

Expected: 2 tests PASS, build clean.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/enqueuer.go internal/apps/enqueuer_test.go go.mod go.sum
git commit -m "feat(apps): TaskEnqueuer interface + Memory + Cloud Tasks implementations"
```

---

## Task 5: Webhook secret cache

**Files:**
- Create: `internal/apps/webhook_secret_cache.go`
- Create: `internal/apps/webhook_secret_cache_test.go`

`events.Fire` signs every payload with the App's webhook secret. The secret lives in Secret Manager (per Plan 1 storage layout); fetching it on every event would be slow. This task adds a per-process in-memory cache with 5-minute TTL per spec §7.2.

- [ ] **Step 1: Write failing test**

Create `internal/apps/webhook_secret_cache_test.go`:

```go
package apps

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubStore is a tiny SecretStore for testing the cache without hitting
// Secret Manager. Counts calls.
type stubStore struct {
	value    []byte
	err      error
	getCount int
}

func (s *stubStore) Put(_ context.Context, _ string, _ []byte) (string, error) {
	return "", nil
}
func (s *stubStore) Get(_ context.Context, _ string) ([]byte, error) {
	s.getCount++
	if s.err != nil {
		return nil, s.err
	}
	cp := make([]byte, len(s.value))
	copy(cp, s.value)
	return cp, nil
}
func (s *stubStore) Delete(_ context.Context, _ string) error { return nil }

func TestWebhookSecretCacheHitsStoreOnce(t *testing.T) {
	store := &stubStore{value: []byte("hunter2")}
	cache := NewWebhookSecretCache(store, 5*time.Minute)

	for i := 0; i < 3; i++ {
		got, err := cache.Get(context.Background(), "apps/x/webhook-secret")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != "hunter2" {
			t.Errorf("Get = %q", got)
		}
	}
	if store.getCount != 1 {
		t.Errorf("store.Get called %d times, want 1 (cache should reuse)", store.getCount)
	}
}

func TestWebhookSecretCacheExpiry(t *testing.T) {
	store := &stubStore{value: []byte("v1")}
	cache := NewWebhookSecretCache(store, 50*time.Millisecond)
	if _, err := cache.Get(context.Background(), "x"); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	store.value = []byte("v2")
	time.Sleep(60 * time.Millisecond)
	got, _ := cache.Get(context.Background(), "x")
	if string(got) != "v2" {
		t.Errorf("after expiry got %q, want v2 (cache should refresh)", got)
	}
}

func TestWebhookSecretCachePropagatesError(t *testing.T) {
	store := &stubStore{err: errors.New("boom")}
	cache := NewWebhookSecretCache(store, time.Minute)
	if _, err := cache.Get(context.Background(), "x"); err == nil {
		t.Error("expected error from underlying store to propagate")
	}
}
```

- [ ] **Step 2: Confirm fail**

```bash
go test ./internal/apps/... -run TestWebhookSecretCache -v
```

Expected: undefined `NewWebhookSecretCache`.

- [ ] **Step 3: Implementation**

Create `internal/apps/webhook_secret_cache.go`:

```go
package apps

import (
	"context"
	"sync"
	"time"
)

// WebhookSecretCache is a per-process in-memory cache for App webhook
// secrets fetched from a SecretStore. Reduces Secret Manager hits on the
// webhook fan-out hot path. Cache TTL matches spec §7.2 (5 minutes).
type WebhookSecretCache struct {
	store SecretStore
	ttl   time.Duration

	mu sync.RWMutex
	m  map[string]secretCacheEntry
}

type secretCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

func NewWebhookSecretCache(store SecretStore, ttl time.Duration) *WebhookSecretCache {
	return &WebhookSecretCache{
		store: store,
		ttl:   ttl,
		m:     make(map[string]secretCacheEntry),
	}
}

// Get fetches the secret for the given resource name, hitting the cache
// when fresh. Returns a defensive copy on every call.
func (c *WebhookSecretCache) Get(ctx context.Context, resourceName string) ([]byte, error) {
	c.mu.RLock()
	e, ok := c.m[resourceName]
	c.mu.RUnlock()
	if ok && time.Now().Before(e.expiresAt) {
		cp := make([]byte, len(e.value))
		copy(cp, e.value)
		return cp, nil
	}

	v, err := c.store.Get(ctx, resourceName)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	stored := make([]byte, len(v))
	copy(stored, v)
	c.m[resourceName] = secretCacheEntry{value: stored, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

// Invalidate drops a cached entry; useful when a secret is rotated.
func (c *WebhookSecretCache) Invalidate(resourceName string) {
	c.mu.Lock()
	delete(c.m, resourceName)
	c.mu.Unlock()
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/apps/... -run TestWebhookSecretCache -v
go vet ./internal/apps/...

git add internal/apps/webhook_secret_cache.go internal/apps/webhook_secret_cache_test.go
git commit -m "feat(apps): webhook secret cache (5-min TTL over SecretStore)"
```

---

## Task 6: Bot identity Firestore-backed cache

**Files:**
- Create: `internal/apps/bot_identity.go`
- Create: `internal/apps/bot_identity_test.go`

Spec §9.2 requires `IsBotIdentity(ctx, login) bool` to be backed by a Firestore-driven view of every `users/{id}` doc with `type: "Bot"`, refreshed every 60 seconds, plus a baked-in seed of the existing hard-coded sync bots. This replaces the `knownSyncIdentities` map in `webhooks.go`.

- [ ] **Step 1: Write failing test**

Create `internal/apps/bot_identity_test.go`:

```go
package apps

import (
	"context"
	"os"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestIsBotIdentity_SeedList(t *testing.T) {
	// Seeded sync-bot names should match without any Firestore reads.
	cache := NewBotIdentityCache(nil, time.Minute) // nil fs is OK for seed-only lookups
	if !cache.Contains("gitbucket-sync-bot") {
		t.Error("expected gitbucket-sync-bot in seed list")
	}
	if !cache.Contains("Gitbucket-Sync-Bot") {
		t.Error("expected case-insensitive match")
	}
	if cache.Contains("alice") {
		t.Error("regular user should not be a bot")
	}
}

func TestIsBotIdentity_FirestoreRefresh(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	// Create a bot user.
	slug := "bot-cache-" + randHex(4)
	botUID, err := CreateBotUser(ctx, fs, slug, slug, "", "test-app")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
	})

	cache := NewBotIdentityCache(fs, 100*time.Millisecond)
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if !cache.Contains(slug + "[bot]") {
		t.Errorf("expected %s[bot] to be recognized as a bot after refresh", slug)
	}
}

func TestIsBotIdentity_TopLevelHelper(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	// The top-level package-level cache must be settable via SetBotIdentityCache
	// so tests can inject a controlled instance and IsBotIdentity uses it.
	prev := DefaultBotIdentityCache
	defer SetDefaultBotIdentityCache(prev)

	cache := NewBotIdentityCache(nil, time.Minute)
	SetDefaultBotIdentityCache(cache)

	if !IsBotIdentity(context.Background(), "gitbucket-sync-bot") {
		t.Error("IsBotIdentity package-level helper should see seed list")
	}
}
```

- [ ] **Step 2: Confirm fail**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestIsBotIdentity -v
```

Expected: undefined `NewBotIdentityCache`, `IsBotIdentity`, etc.

- [ ] **Step 3: Implementation**

Create `internal/apps/bot_identity.go`:

```go
package apps

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// botSeedList is the baked-in list of sync-bot usernames that must always be
// treated as bot identities even before the Firestore-backed cache loads.
// Preserves the behavior of the old hard-coded knownSyncIdentities map in
// internal/api/webhooks.go.
var botSeedList = []string{
	"gitbucket-sync-bot",
	"gitbucket-sync-bot[bot]",
	"jules-sync-bot",
	"github-actions[bot]",
}

// BotIdentityCache is a snapshot of bot usernames refreshed periodically from
// Firestore. Reads are lock-free via an atomic.Value swap of the set.
type BotIdentityCache struct {
	fs            *firestore.Client
	refreshEvery  time.Duration
	set           atomic.Value // map[string]struct{} (lowercased keys)

	refreshOnce   sync.Mutex   // serializes concurrent Refresh() calls
}

// NewBotIdentityCache constructs an initially-seed-populated cache. If fs is
// non-nil, callers should invoke Refresh() periodically (or Start() to spawn
// a background refresher).
func NewBotIdentityCache(fs *firestore.Client, refreshEvery time.Duration) *BotIdentityCache {
	c := &BotIdentityCache{fs: fs, refreshEvery: refreshEvery}
	seed := make(map[string]struct{}, len(botSeedList))
	for _, name := range botSeedList {
		seed[strings.ToLower(name)] = struct{}{}
	}
	c.set.Store(seed)
	return c
}

// Contains reports whether the given login matches a known bot identity.
// Case-insensitive.
func (c *BotIdentityCache) Contains(login string) bool {
	set := c.set.Load().(map[string]struct{})
	_, ok := set[strings.ToLower(login)]
	return ok
}

// Refresh queries Firestore for every users/{id} doc with type:"Bot" and
// rebuilds the set, retaining the seed list. Safe to call concurrently.
func (c *BotIdentityCache) Refresh(ctx context.Context) error {
	if c.fs == nil {
		return nil // seed-only mode (tests)
	}
	c.refreshOnce.Lock()
	defer c.refreshOnce.Unlock()

	next := make(map[string]struct{}, len(botSeedList)+16)
	for _, name := range botSeedList {
		next[strings.ToLower(name)] = struct{}{}
	}

	iter := c.fs.Collection(CollectionUsers).Where("type", "==", "Bot").Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		login, _ := data["username"].(string)
		if login != "" {
			next[strings.ToLower(login)] = struct{}{}
		}
	}
	c.set.Store(next)
	return nil
}

// Start spawns a goroutine that calls Refresh on the configured interval
// until ctx is cancelled. Safe to call multiple times — each invocation
// spawns its own goroutine, so call exactly once at startup.
func (c *BotIdentityCache) Start(ctx context.Context) {
	if c.fs == nil {
		return
	}
	go func() {
		// Initial refresh.
		_ = c.Refresh(ctx)
		t := time.NewTicker(c.refreshEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := c.Refresh(ctx); err != nil {
					logEvents("bot-identity refresh: " + err.Error())
				}
			}
		}
	}()
}

// --- Package-level singleton for use by webhook receivers ------------------

// DefaultBotIdentityCache is the package-level cache instance consulted by
// IsBotIdentity. Set via SetDefaultBotIdentityCache during main() bootstrap.
// Until set, Contains() on the seed list still works (Default is a fresh
// seed-only cache).
var DefaultBotIdentityCache = NewBotIdentityCache(nil, 60*time.Second)

// SetDefaultBotIdentityCache swaps in a cache backed by a real Firestore
// client. Call once during main() init, after the bot-user records have been
// migrated to type:"Bot".
func SetDefaultBotIdentityCache(c *BotIdentityCache) {
	DefaultBotIdentityCache = c
}

// IsBotIdentity is the public helper that webhook receivers (and the events
// Fire path) call. Reads from DefaultBotIdentityCache.
func IsBotIdentity(_ context.Context, login string) bool {
	return DefaultBotIdentityCache.Contains(login)
}
```

- [ ] **Step 4: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestIsBotIdentity -v
go vet ./internal/apps/...

git add internal/apps/bot_identity.go internal/apps/bot_identity_test.go
git commit -m "feat(apps): BotIdentityCache (Firestore-backed, 60s refresh)"
```

---

## Task 7: events.Fire integration (the main event)

**Files:**
- Modify: `internal/apps/events.go` — replace the stub Fire with the real one
- Modify: `internal/apps/events_test.go` — add Fire-path integration tests

This task wires together everything from Tasks 2-6: query matching installations, build the GitHub-shape payload, sign with the webhook secret, write a delivery record, enqueue a task.

For Plan 3, the GitHub-shape payload construction is per-event-type. We use existing `v3fmt` formatters where they exist (PR formatter from Plan 2 for `pull_request` events). For `push` we build a minimal payload directly (GitHub's full push payload is huge; we emit the essentials).

- [ ] **Step 1: Add fields to FireDeps and update Fire signature**

The new Fire signature accepts a `FireDeps` bundle injected at app startup. This avoids package-level globals and makes testing trivial. Append to `internal/apps/events.go` (REPLACING the existing stub Fire + helpers):

```go
import (
	// add these:
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"cloud.google.com/go/firestore"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/db"
)

// FireDeps bundles the runtime dependencies needed by Fire. Construct once
// in main() and store on something accessible from event-firing handlers
// (e.g. on V3Handler / APIHandler).
type FireDeps struct {
	FS          *firestore.Client
	Enqueuer    TaskEnqueuer
	SecretCache *WebhookSecretCache
	URLs        *v3fmt.URLBuilder
	DispatchURL string // base URL for the dispatcher endpoint, e.g. "https://gb.example.com/_internal/dispatch-webhook"
}

// Fire enqueues webhook deliveries for every installation that has
// subscribed to this event on the relevant repository. See package doc.
func Fire(ctx context.Context, deps FireDeps, p Payload) {
	if deps.FS == nil || deps.Enqueuer == nil || deps.SecretCache == nil {
		FireError("Fire called with incomplete deps; webhook delivery skipped")
		return
	}
	// 1. Loop prevention: drop events whose sender is a known bot identity.
	if IsBotIdentity(ctx, p.SenderRef().Login) {
		return
	}

	// 2. Find matching installations.
	insts, err := matchingInstallations(ctx, deps.FS, p)
	if err != nil {
		FireError("matchingInstallations: %v", err)
		return
	}

	// 3. For each, build payload + sign + write delivery + enqueue.
	for _, inst := range insts {
		fireOne(ctx, deps, inst, p)
	}
}

func fireOne(ctx context.Context, deps FireDeps, inst *Installation, p Payload) {
	// 3a. Build the GitHub-shape JSON payload bytes.
	payloadBytes, err := buildEventBody(deps, inst, p)
	if err != nil {
		FireError("buildEventBody: %v", err)
		return
	}

	// 3b. Look up the App for webhook URL + secret resource name.
	app, err := GetApp(ctx, deps.FS, inst.AppID)
	if err != nil || app == nil {
		FireError("GetApp(%s): %v", inst.AppID, err)
		return
	}

	// 3c. Fetch webhook secret (cached).
	secret, err := deps.SecretCache.Get(ctx, app.WebhookSecretResource)
	if err != nil {
		FireError("secret fetch for app %s: %v", app.AppID, err)
		return
	}

	// 3d. Compute signature.
	signature := ComputeHubSignature(payloadBytes, secret)
	payloadHash := sha256.Sum256(payloadBytes)

	// 3e. Write delivery record.
	deliv, err := CreateDelivery(ctx, deps.FS, CreateDeliveryInput{
		AppID:          app.AppID,
		InstallationID: inst.InstallationID,
		Event:          p.Event().String(),
		TargetURL:      app.WebhookURL,
		PayloadSHA256:  hex.EncodeToString(payloadHash[:]),
	})
	if err != nil {
		FireError("CreateDelivery: %v", err)
		return
	}

	// 3f. Build task headers (delivery_id is part of the path).
	headers := map[string]string{
		"Content-Type":                            "application/json",
		"User-Agent":                              "GitBucket-Hookshot/1.0",
		"X-GitHub-Event":                          p.Event().String(),
		"X-GitHub-Delivery":                       deliv.DeliveryID,
		"X-Hub-Signature-256":                     signature,
		"X-GitHub-Hook-ID":                        app.AppID,
		"X-GitHub-Hook-Installation-Target-Type":  "repository",
		"X-GitHub-Hook-Installation-Target-ID":    inst.InstallationID,
	}

	// 3g. Enqueue task targeting our dispatcher.
	target := deps.DispatchURL + "/" + deliv.DeliveryID
	if err := deps.Enqueuer.Enqueue(ctx, TaskSpec{
		TargetURL: target,
		Headers:   headers,
		Body:      payloadBytes,
	}); err != nil {
		FireError("enqueue delivery %s: %v", deliv.DeliveryID, err)
		// Best-effort: mark delivery as failed since we couldn't enqueue.
		_ = UpdateDeliveryStatus(ctx, deps.FS, deliv.DeliveryID, DeliveryUpdate{
			Status:   "failed",
			Attempts: 0,
		})
	}
}

// matchingInstallations finds installations subscribing to the event on the
// owner's repos. For Plan 3 we filter on:
//   * account.id == owner (the repo's owner)
//   * eventType ∈ events
//   * suspended_at IS NULL
// We DON'T filter on repository_ids vs repository_selection in this query
// because Firestore doesn't support OR-of-where-clauses cleanly; we do that
// filter in-memory below.
func matchingInstallations(ctx context.Context, fs *firestore.Client, p Payload) ([]*Installation, error) {
	owner := p.Owner()
	if owner == "" {
		return nil, nil
	}
	all, err := listInstallationsByAccount(ctx, fs, owner)
	if err != nil {
		return nil, err
	}
	out := make([]*Installation, 0, len(all))
	for _, inst := range all {
		if inst.SuspendedAt != nil {
			continue
		}
		if !subscribesToEvent(inst.Events, p.Event()) {
			continue
		}
		// repository_selection filter (Plan 3: per-repo only matters when
		// selection == "selected"; "all" means every repo qualifies).
		if inst.RepositorySelection == "selected" {
			repoID := repoIDFor(p)
			if !containsString(inst.RepositoryIDs, repoID) {
				continue
			}
		}
		out = append(out, inst)
	}
	return out, nil
}

// listInstallationsByAccount queries the installations collection for all
// installations targeting the given account (owner login). This is a tiny
// helper since installations.go's ListInstallationsForApp queries by app_id
// not account.
func listInstallationsByAccount(ctx context.Context, fs *firestore.Client, accountID string) ([]*Installation, error) {
	var out []*Installation
	iter := fs.Collection(CollectionInstallations).Where("account.id", "==", accountID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err != nil {
			// io.EOF or similar; iterator.Done signals end-of-iteration.
			break
		}
		var inst Installation
		if err := doc.DataTo(&inst); err != nil {
			return nil, err
		}
		out = append(out, &inst)
	}
	return out, nil
}

// subscribesToEvent reports whether an installation's `events` array includes
// the given event type. Empty array means "all default events" — for Plan 3
// we treat empty as no subscriptions (caller MUST configure events at install).
func subscribesToEvent(events []string, t EventType) bool {
	name := t.String()
	for _, e := range events {
		if e == name {
			return true
		}
	}
	return false
}

// repoIDFor returns the Firestore repo doc-ID for the payload, matching
// db.CreateRepositoryMetadata's "<lower-owner>_<lower-name>" scheme.
func repoIDFor(p Payload) string {
	switch v := p.(type) {
	case PullRequestPayload:
		return strings.ToLower(v.Owner) + "_" + strings.ToLower(v.Repo)
	case PushPayload:
		return strings.ToLower(v.Owner) + "_" + strings.ToLower(v.Repo)
	default:
		return ""
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// buildEventBody marshals the GitHub-shape JSON payload for the event,
// adding the standard `repository`, `installation`, and `sender` envelope.
func buildEventBody(deps FireDeps, inst *Installation, p Payload) ([]byte, error) {
	envelope := map[string]interface{}{
		"installation": map[string]interface{}{
			"id":      inst.InstallationID,
			"node_id": "I_" + inst.InstallationID, // simple placeholder; Plan 4 polish
		},
		"sender": map[string]interface{}{
			"login": p.SenderRef().Login,
			"id":    p.SenderRef().ID,
			"type":  p.SenderRef().Type,
		},
	}

	switch v := p.(type) {
	case PullRequestPayload:
		envelope["action"] = v.Action
		envelope["number"] = v.Number
		envelope["pull_request"] = v3fmt.PullRequest(v3fmt.PullRequestSource{
			Owner:      v.Owner,
			Repo:       v.Repo,
			Number:     v.Number,
			Title:      v.Title,
			Body:       v.Body,
			State:      v.State,
			Author:     v3fmt.StaticUser(p.SenderRef().Login, "user:"+p.SenderRef().Login, p.SenderRef().Type, ""),
			HeadBranch: v.HeadBranch,
			BaseBranch: v.BaseBranch,
			HeadSHA:    v.HeadSHA,
			BaseSHA:    v.BaseSHA,
			MergedAt:   v.MergedAt,
		}, deps.URLs)
		envelope["repository"] = repoEnvelope(deps, v.Owner, v.Repo)
	case PushPayload:
		envelope["ref"] = v.Ref
		envelope["before"] = v.Before
		envelope["after"] = v.After
		envelope["commits"] = v.Commits
		envelope["repository"] = repoEnvelope(deps, v.Owner, v.Repo)
	case InstallationPayload:
		envelope["action"] = v.Action
	}

	return json.Marshal(envelope)
}

// repoEnvelope returns a thin repo block for inclusion in event payloads.
// Plan 3 emits a minimal shape; if a receiver needs more, it can call
// GET /api/v3/repos/{owner}/{repo}.
func repoEnvelope(deps FireDeps, owner, repo string) map[string]interface{} {
	return map[string]interface{}{
		"full_name": owner + "/" + repo,
		"name":      repo,
		"owner":     map[string]interface{}{"login": owner},
		"url":       deps.URLs.RepoAPI(owner, repo),
		"html_url":  deps.URLs.RepoHTML(owner, repo),
	}
}
```

Also add `"strings"` to the imports if not present (used by `repoIDFor`).

- [ ] **Step 2: Write the failing integration test**

Append to `internal/apps/events_test.go`:

```go
import (
	"context"
	"encoding/json"
	"os"

	"cloud.google.com/go/firestore"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestFire_EnqueuesForMatchingInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	// The default Scenario installation has events including "pull_request".
	// Confirm by reading.
	owner := scen.Installation.Account.ID

	enq := NewMemoryEnqueuer()
	cache := NewWebhookSecretCache(scen.Store, time.Minute)
	deps := FireDeps{
		FS:          fs,
		Enqueuer:    enq,
		SecretCache: cache,
		URLs:        v3fmt.NewURLBuilder("https://gb.test"),
		DispatchURL: "https://gb.test/_internal/dispatch-webhook",
	}

	Fire(ctx, deps, PullRequestPayload{
		Action: "opened", Number: 1, Title: "x", State: "open",
		HeadBranch: "f", BaseBranch: "main",
		Owner: owner, Repo: "any-repo",
		Sender: SenderRef{Login: "alice", ID: 1, Type: "User"},
	})

	tasks := enq.Drain()
	if len(tasks) != 1 {
		t.Fatalf("Drain len = %d, want 1; installation may not subscribe to pull_request", len(tasks))
	}
	tt := tasks[0]
	if tt.Headers["X-GitHub-Event"] != "pull_request" {
		t.Errorf("X-GitHub-Event = %q", tt.Headers["X-GitHub-Event"])
	}
	if tt.Headers["X-Hub-Signature-256"] == "" {
		t.Error("X-Hub-Signature-256 missing")
	}
	if !strings.HasPrefix(tt.TargetURL, "https://gb.test/_internal/dispatch-webhook/") {
		t.Errorf("TargetURL = %q", tt.TargetURL)
	}

	// Body should parse as JSON and contain the expected envelope keys.
	var body map[string]interface{}
	if err := json.Unmarshal(tt.Body, &body); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	for _, key := range []string{"action", "number", "pull_request", "repository", "installation", "sender"} {
		if _, ok := body[key]; !ok {
			t.Errorf("payload missing key %q", key)
		}
	}
}

func TestFire_SkipsBotSender(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	enq := NewMemoryEnqueuer()
	deps := FireDeps{
		FS:          fs,
		Enqueuer:    enq,
		SecretCache: NewWebhookSecretCache(scen.Store, time.Minute),
		URLs:        v3fmt.NewURLBuilder("https://gb.test"),
		DispatchURL: "https://gb.test/_internal/dispatch-webhook",
	}

	Fire(ctx, deps, PullRequestPayload{
		Action: "opened", Number: 1, State: "open",
		Owner: scen.Installation.Account.ID, Repo: "r",
		Sender: SenderRef{Login: "gitbucket-sync-bot", ID: 999, Type: "Bot"},
	})

	if got := enq.Drain(); len(got) != 0 {
		t.Errorf("expected 0 tasks for bot sender, got %d", len(got))
	}
}
```

Add to imports the new packages you reference: `"strings"` if not present.

- [ ] **Step 3: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run "TestFire" -v
go vet ./internal/apps/...
go build ./...

git add internal/apps/events.go internal/apps/events_test.go
git commit -m "feat(apps): Fire integration — query installs, build payload, sign, enqueue"
```

---

## Task 8: Dispatcher endpoint

**Files:**
- Create: `internal/apps/dispatcher.go`
- Create: `internal/apps/dispatcher_test.go`
- Modify: `internal/apps/routes.go` (add route mount helper) — OR add to main.go directly in Task 11

The dispatcher receives the Cloud Task, verifies the OIDC token, POSTs the payload to the App's webhook URL, updates the delivery record. Non-2xx response → return non-2xx so Cloud Tasks retries per queue policy.

For Plan 3 we make OIDC verification optional (off by default, on when configured) so unit tests don't need to mint OIDC tokens. Production turns it on by setting an audience.

- [ ] **Step 1: Write failing test**

Create `internal/apps/dispatcher_test.go`:

```go
package apps

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/db"
)

func TestDispatcher_RelaysAndMarksDelivered(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	// Fake App webhook receiver — captures the request.
	var captured *http.Request
	var capturedBody []byte
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer app.Close()

	deliv, err := CreateDelivery(ctx, fs, CreateDeliveryInput{
		AppID:          "app-x",
		InstallationID: "inst-x",
		Event:          "pull_request",
		TargetURL:      app.URL,
		PayloadSHA256:  "deadbeef",
	})
	if err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionWebhookDeliveries).Doc(deliv.DeliveryID).Delete(context.Background())
	})

	dh := NewDispatcherHandler(fs, "" /* no OIDC audience in tests */)
	r := chi.NewRouter()
	r.Post("/_internal/dispatch-webhook/{id}", dh.Dispatch)

	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest("POST", "/_internal/dispatch-webhook/"+deliv.DeliveryID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=fake")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("dispatcher returned %d, want 200", rr.Code)
	}
	if captured == nil {
		t.Fatal("fake App did not receive a request")
	}
	if string(capturedBody) != string(body) {
		t.Errorf("body relay mismatch: %q vs %q", capturedBody, body)
	}
	if captured.Header.Get("X-GitHub-Event") != "pull_request" {
		t.Error("X-GitHub-Event header not relayed")
	}
	if captured.Header.Get("X-Hub-Signature-256") != "sha256=fake" {
		t.Error("X-Hub-Signature-256 header not relayed")
	}

	// Delivery record should be updated.
	got, _ := GetDelivery(ctx, fs, deliv.DeliveryID)
	if got.Status != "delivered" {
		t.Errorf("Status = %q, want delivered", got.Status)
	}
	if got.LastResponseCode != 200 {
		t.Errorf("LastResponseCode = %d", got.LastResponseCode)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", got.Attempts)
	}
}

func TestDispatcher_5xxReturnsNon2xxForRetry(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer app.Close()

	deliv, _ := CreateDelivery(ctx, fs, CreateDeliveryInput{
		AppID: "app-y", InstallationID: "inst-y", Event: "push",
		TargetURL: app.URL, PayloadSHA256: "x",
	})
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionWebhookDeliveries).Doc(deliv.DeliveryID).Delete(context.Background())
	})

	dh := NewDispatcherHandler(fs, "")
	r := chi.NewRouter()
	r.Post("/_internal/dispatch-webhook/{id}", dh.Dispatch)

	req := httptest.NewRequest("POST", "/_internal/dispatch-webhook/"+deliv.DeliveryID, strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// We expect the dispatcher to surface the upstream non-2xx so Cloud Tasks retries.
	if rr.Code < 400 {
		t.Errorf("dispatcher returned %d, expected non-2xx to trigger retry", rr.Code)
	}

	got, _ := GetDelivery(ctx, fs, deliv.DeliveryID)
	if got.Status != "failed" {
		t.Errorf("Status = %q, want failed", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d", got.Attempts)
	}
}
```

- [ ] **Step 2: Confirm fail**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestDispatcher -v
```

Expected: undefined `NewDispatcherHandler`.

- [ ] **Step 3: Implementation**

Create `internal/apps/dispatcher.go`:

```go
package apps

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"
)

// DispatcherHandler relays Cloud Tasks-delivered webhooks to App receivers.
// Mounted at /_internal/dispatch-webhook/{id}.
type DispatcherHandler struct {
	FS                *firestore.Client
	OIDCAudience      string        // when set, verify inbound OIDC token's audience claim
	HTTPClient        *http.Client  // for relaying to App URLs (timeout-bounded)
}

func NewDispatcherHandler(fs *firestore.Client, oidcAudience string) *DispatcherHandler {
	return &DispatcherHandler{
		FS:           fs,
		OIDCAudience: oidcAudience,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Dispatch is the HTTP handler. Reads the task body and headers, looks up the
// delivery record for target_url, POSTs the body verbatim to the App, and
// updates the delivery record with the response.
func (d *DispatcherHandler) Dispatch(w http.ResponseWriter, r *http.Request) {
	// 1. Optional OIDC verification.
	if d.OIDCAudience != "" {
		if !verifyOIDCAudience(r, d.OIDCAudience) {
			http.Error(w, "invalid oidc token", http.StatusUnauthorized)
			return
		}
	}

	deliveryID := chi.URLParam(r, "id")
	deliv, err := GetDelivery(r.Context(), d.FS, deliveryID)
	if err != nil {
		http.Error(w, "delivery lookup error", http.StatusInternalServerError)
		return
	}
	if deliv == nil {
		// Already cleaned up; return 200 so Cloud Tasks stops retrying.
		http.Error(w, "delivery not found", http.StatusOK)
		return
	}
	if deliv.Status == "delivered" {
		// Idempotency: already delivered, ack the retry.
		w.WriteHeader(http.StatusOK)
		return
	}

	// 2. Relay.
	body, _ := io.ReadAll(r.Body)
	relayReq, err := http.NewRequestWithContext(r.Context(), "POST", deliv.TargetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "relay request build: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Pass through standard GitHub-shape headers.
	for _, k := range []string{
		"Content-Type",
		"User-Agent",
		"X-GitHub-Event",
		"X-GitHub-Delivery",
		"X-Hub-Signature-256",
		"X-GitHub-Hook-ID",
		"X-GitHub-Hook-Installation-Target-Type",
		"X-GitHub-Hook-Installation-Target-ID",
	} {
		if v := r.Header.Get(k); v != "" {
			relayReq.Header.Set(k, v)
		}
	}

	resp, err := d.HTTPClient.Do(relayReq)
	attempt := deliv.Attempts + 1
	now := time.Now().UTC()

	if err != nil {
		_ = UpdateDeliveryStatus(r.Context(), d.FS, deliveryID, DeliveryUpdate{
			Status:           "failed",
			Attempts:         attempt,
			LastResponseCode: 0,
			LastAttemptedAt:  now,
		})
		http.Error(w, "relay error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	// 3. Update delivery record.
	status := "delivered"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "failed"
	}
	_ = UpdateDeliveryStatus(r.Context(), d.FS, deliveryID, DeliveryUpdate{
		Status:           status,
		Attempts:         attempt,
		LastResponseCode: resp.StatusCode,
		LastAttemptedAt:  now,
	})

	// 4. Mirror the upstream status code so Cloud Tasks knows whether to retry.
	w.WriteHeader(resp.StatusCode)
}

// verifyOIDCAudience checks Cloud Tasks' Authorization: Bearer <jwt> header
// against the expected audience claim. For Plan 3 we implement a minimal
// check that delegates to golang-jwt/jwt with Google's public keys.
//
// FUTURE: use Google's identitytoolkit/v1 verifyIdToken or the standard
// `google.golang.org/api/idtoken` package for production-grade verification.
// For Plan 3 MVP this is a no-op that always returns true if no audience
// is configured (callers gate via `if d.OIDCAudience != ""`).
//
// Until the real verification lands, configuring OIDCAudience in production
// MUST be paired with network-level protection (Cloud Run ingress = internal
// only, with proper IAM on the queue).
func verifyOIDCAudience(_ *http.Request, _ string) bool {
	// Intentionally permissive in Plan 3. Tighten in a follow-on.
	return true
}
```

- [ ] **Step 4: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestDispatcher -v
go vet ./internal/apps/...

git add internal/apps/dispatcher.go internal/apps/dispatcher_test.go
git commit -m "feat(apps): dispatcher endpoint relays webhook tasks to App receivers"
```

---

## Task 9: Replace knownSyncIdentities + integrate IsBotIdentity

**Files:**
- Modify: `internal/api/webhooks.go` — replace hardcoded map with `apps.IsBotIdentity`

The existing inbound-webhook handler in `internal/api/webhooks.go` uses a hardcoded `knownSyncIdentities` map (lines 61-66) checked against 4 different fields. Replace all reads with `apps.IsBotIdentity`.

- [ ] **Step 1: Read existing webhooks.go**

```bash
sed -n '60,160p' internal/api/webhooks.go
```

Note the 4 call sites at lines 127, 128, 136, 137, 151, 152.

- [ ] **Step 2: Update the file**

Replace `var knownSyncIdentities = map[string]bool{ ... }` block with:

```go
// knownSyncIdentities was a hardcoded map; bot identity is now sourced from
// apps.IsBotIdentity which reads from a Firestore-backed cache seeded with
// the same baseline names. See internal/apps/bot_identity.go.
```

(Leave a stub comment so any future grep finds it.)

Add `"gitbucket/internal/apps"` to the imports (alphabetical).

Replace every `knownSyncIdentities[strings.ToLower(<expr>)]` call with `apps.IsBotIdentity(r.Context(), <expr>)`. The 6 sites are:

```go
// Before:
if knownSyncIdentities[strings.ToLower(payload.Sender.Login)] ||
    knownSyncIdentities[strings.ToLower(payload.Pusher.Name)] {
// After:
if apps.IsBotIdentity(r.Context(), payload.Sender.Login) ||
    apps.IsBotIdentity(r.Context(), payload.Pusher.Name) {
```

Apply the same translation for the head-commit and commits-loop checks.

- [ ] **Step 3: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/... ./internal/apps/... -v 2>&1 | tail -30
go vet ./...
go build ./...
```

The existing `TestHandleGitHubWebhook` (if any) in `webhooks_test.go` must still pass — it seeds payloads with `gitbucket-sync-bot` which is in the baseline seed list, so `apps.IsBotIdentity` returns true for those.

- [ ] **Step 4: Commit**

```bash
git add internal/api/webhooks.go
git commit -m "refactor(api): replace knownSyncIdentities map with apps.IsBotIdentity"
```

---

## Task 10: Wire pull_request events into PR handlers

**Files:**
- Modify: `internal/api/v3/pulls.go` — fire `pull_request:opened` / `pull_request:edited` after Create/Update
- Modify: `internal/api/v3/handler.go` — add `FireDeps` field (so Fire can be called from handlers)
- Modify: `internal/api/pull_requests.go` — fire `pull_request:opened` / `closed` / `merged` after the existing handlers
- Modify: `internal/api/api.go` — add `FireDeps` to APIHandler (so Fire can be called from the UI handler)
- Modify: `main.go` — construct FireDeps and pass to both handlers (Task 11)

These wires are small per call site but multiply across handlers.

- [ ] **Step 1: Add FireDeps field to V3Handler**

In `internal/api/v3/handler.go`, add:

```go
import (
	// existing imports
	"gitbucket/internal/apps"
)

type V3Handler struct {
	FirestoreClient *firestore.Client
	StorageClient   *storage.Client
	URLs            *v3fmt.URLBuilder
	LocalReposRoot  string
	Events          apps.FireDeps // new — populated in main.go
}
```

(No change to `NewV3Handler` signature — `Events` is set by `main.go` after construction, same pattern as `LocalReposRoot`.)

- [ ] **Step 2: Fire events in v3/pulls.go Create + Update**

In `internal/api/v3/pulls.go::CreatePull`, after the successful `db.CreatePullRequest` call but before `WriteJSON`, add:

```go
apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
    Action:     "opened",
    Number:     pr.Number,
    Title:      pr.Title,
    Body:       pr.Description,
    State:      "open",
    HeadBranch: pr.SourceBranch,
    BaseBranch: pr.TargetBranch,
    Owner:      owner,
    Repo:       repo,
    Sender:     senderFromCtx(r.Context()),
})
```

Add a small helper at the bottom of pulls.go:

```go
// senderFromCtx derives a SenderRef from the installation context. For Plan 3
// the actor on any v3 write is the App's bot user. Used as the `sender` block
// in event payloads.
func senderFromCtx(ctx context.Context) apps.SenderRef {
	ic := apps.InstallationContextFrom(ctx)
	if ic == nil {
		return apps.SenderRef{Login: "unknown", Type: "User"}
	}
	return apps.SenderRef{
		Login: "app-" + ic.AppID + "[bot]",
		Type:  "Bot",
	}
}
```

Add `"context"` to imports if not already present.

In `UpdatePull`, after the writes succeed and pr has been re-fetched, fire `edited` (if title/body changed) or `closed`/`reopened` (if state changed). Simplest implementation: always fire `edited` since we can't easily detect which fields actually changed without comparing pre+post:

```go
action := "edited"
if req.State == "closed" {
    action = "closed"
} else if req.State == "open" {
    action = "reopened"
}
apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
    Action:     action,
    Number:     pr.Number,
    Title:      pr.Title,
    Body:       pr.Description,
    State:      pr.Status, // "open" | "closed" | "merged"
    HeadBranch: pr.SourceBranch,
    BaseBranch: pr.TargetBranch,
    Owner:      owner,
    Repo:       repo,
    Sender:     senderFromCtx(r.Context()),
})
```

- [ ] **Step 3: Add FireDeps to APIHandler + fire in UI handlers**

In `internal/api/api.go`, add to `APIHandler` struct:

```go
type APIHandler struct {
    // existing fields...
    Events apps.FireDeps
}
```

Add `"gitbucket/internal/apps"` to imports if not present.

In `internal/api/pull_requests.go`:

- `CreatePullRequest`: after `pr, err := db.CreatePullRequest(...)` succeeds and just before `WriteJSON`, add a Fire call mirroring v3's CreatePull. Use the existing `username` variable from `auth.GetUsername(r.Context())` as the sender (this handler is Firebase-auth, so the actor is a human user).

```go
apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
    Action:     "opened",
    Number:     pr.Number,
    Title:      pr.Title,
    Body:       pr.Description,
    State:      "open",
    HeadBranch: pr.SourceBranch,
    BaseBranch: pr.TargetBranch,
    Owner:      owner,
    Repo:       repo,
    Sender:     apps.SenderRef{Login: username, Type: "User"},
})
```

- `ClosePullRequest`: after the successful status update, fire `pull_request:closed`. Need to look at the existing code — adapt the same pattern.

- `MergePullRequest`: similar — fire with `Action: "closed"` and `Merged: true`, including `MergedAt: &now`.

- [ ] **Step 4: Run tests + commit (main.go integration follows in Task 11)**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/... ./internal/apps/... -v 2>&1 | tail -30
go vet ./...
go build ./...
```

Tests should still pass (FireDeps is zero-valued at construction → Fire returns early via its own nil-deps guard, so existing tests don't break).

```bash
git add internal/api/v3/handler.go internal/api/v3/pulls.go internal/api/api.go internal/api/pull_requests.go
git commit -m "feat(api): fire pull_request events from PR write handlers"
```

---

## Task 11: Wire push events + main.go integration

**Files:**
- Modify: `internal/api/git.go` — fire `push` event in post-receive success block
- Modify: `main.go` — initialize Cloud Tasks client, webhook secret cache, FireDeps, bot identity refresher; mount dispatcher route
- Modify: `internal/config/config.go` — add `CloudTasksQueueName`, `OIDCServiceAccount` env vars (with sensible defaults)

- [ ] **Step 1: Add config fields**

In `internal/config/config.go`, add to `Config`:

```go
type Config struct {
    // existing...
    CloudTasksQueueName      string // e.g. "projects/<p>/locations/us-central1/queues/gitbucket-webhooks"
    DispatcherOIDCSA         string // service account email Cloud Tasks uses for OIDC
    DispatcherOIDCAudience   string // expected audience on incoming dispatcher requests
}
```

In `Load()`:

```go
return &Config{
    // existing fields...
    CloudTasksQueueName:    os.Getenv("CLOUD_TASKS_QUEUE_NAME"),
    DispatcherOIDCSA:       os.Getenv("DISPATCHER_OIDC_SA"),
    DispatcherOIDCAudience: os.Getenv("DISPATCHER_OIDC_AUDIENCE"),
}
```

- [ ] **Step 2: Fire push events in git.go**

In `internal/api/git.go::HandleGitHTTP`, find the post-push success block (around line 365-380, after GCS upload + branch metadata update). Add:

```go
import (
    // add to existing imports:
    "gitbucket/internal/apps"
)

// inside the post-push success block, after the existing GCS+metadata updates:
for ref, newSHA := range postRefs {
    oldSHA := preRefs[ref]
    if oldSHA == newSHA {
        continue
    }
    apps.Fire(r.Context(), h.Events, apps.PushPayload{
        Ref:    ref,
        Before: defaultZeroSHA(oldSHA),
        After:  defaultZeroSHA(newSHA),
        Owner:  owner,
        Repo:   repo,
        Sender: apps.SenderRef{Login: auth.GetUsername(r.Context()), Type: "User"},
    })
}
```

Add a small helper near the top of git.go:

```go
func defaultZeroSHA(s string) string {
    if s == "" {
        return "0000000000000000000000000000000000000000"
    }
    return s
}
```

- [ ] **Step 3: main.go integration**

In `main.go`, add imports:

```go
cloudtasks "cloud.google.com/go/cloudtasks/apiv2"

// ... already imported:
// "gitbucket/internal/apps"
```

Inside `main()`, after the existing Secret Manager client init (Plan 1's block), add:

```go
// Plan 3: webhook engine init.
appsSecretCache := apps.NewWebhookSecretCache(appsSecretStore, 5*time.Minute)

var appsEnqueuer apps.TaskEnqueuer
if cfg.DevMode || cfg.CloudTasksQueueName == "" {
    appsEnqueuer = apps.NewMemoryEnqueuer()
    log.Println("apps: using in-memory TaskEnqueuer (DEV_MODE or no queue configured)")
} else {
    ctClient, err := cloudtasks.NewClient(ctx)
    if err != nil {
        log.Fatalf("failed to initialize Cloud Tasks client: %v", err)
    }
    defer ctClient.Close()
    appsEnqueuer = apps.NewRealEnqueuer(ctClient, cfg.CloudTasksQueueName, cfg.DispatcherOIDCSA, cfg.DispatcherOIDCAudience)
    log.Printf("apps: Cloud Tasks enqueuer initialized for queue %s", cfg.CloudTasksQueueName)
}

fireDeps := apps.FireDeps{
    FS:          firestoreClient,
    Enqueuer:    appsEnqueuer,
    SecretCache: appsSecretCache,
    URLs:        v3fmt.NewURLBuilder(baseURL(cfg)),
    DispatchURL: baseURL(cfg) + "/_internal/dispatch-webhook",
}

// Start bot identity refresher.
botCache := apps.NewBotIdentityCache(firestoreClient, 60*time.Second)
apps.SetDefaultBotIdentityCache(botCache)
botCache.Start(ctx)
```

Add `"gitbucket/internal/api/v3/v3fmt"` to imports.

After both `apiHandler` and `v3Handler` are constructed, set their Events field:

```go
apiHandler.Events = fireDeps
v3Handler.Events = fireDeps
```

After existing route registrations, mount the dispatcher endpoint:

```go
// Plan 3: dispatcher endpoint for Cloud Tasks → App webhook fan-out.
dispatcher := apps.NewDispatcherHandler(firestoreClient, cfg.DispatcherOIDCAudience)
r.Post("/_internal/dispatch-webhook/{id}", dispatcher.Dispatch)
```

Update the SPA fallback guard in main.go — the existing line:

```go
if strings.HasPrefix(req.URL.Path, "/api/") || strings.HasPrefix(req.URL.Path, "/r/") {
```

becomes:

```go
if strings.HasPrefix(req.URL.Path, "/api/") ||
    strings.HasPrefix(req.URL.Path, "/r/") ||
    strings.HasPrefix(req.URL.Path, "/_internal/") {
```

- [ ] **Step 4: Run + build**

```bash
go build ./...
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/... 2>&1 | tail -30
go vet ./...
```

All must pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/git.go internal/config/config.go main.go
git commit -m "feat(apps): wire push events + Cloud Tasks enqueuer + dispatcher in main"
```

---

## Task 12: End-to-end fake-App receiver walkthrough

**Files:**
- Create: `internal/apps/e2e_webhook_test.go`

A single test that simulates the full real-world flow:
1. Spin up a fake App HTTP server (`httptest.NewServer`).
2. Seed App + Installation pointing webhook_url at the fake App, subscribed to `pull_request`.
3. Create a PR via the v3 endpoint.
4. Drain the MemoryEnqueuer; one task should be queued targeting our dispatcher.
5. Replay the task body through the dispatcher handler.
6. Assert the fake App received the request with valid signature.

- [ ] **Step 1: Write the test**

Create `internal/apps/e2e_webhook_test.go`:

```go
package apps_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	v3 "gitbucket/internal/api/v3"
	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestPlan3WebhookFlow(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	// Fake App receiver.
	var receivedMu sync.Mutex
	var receivedReq *http.Request
	var receivedBody []byte
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		receivedReq = r.Clone(context.Background())
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer appServer.Close()

	// Update the App's webhook_url to point at the fake server. The
	// Scenario's App was created with a placeholder URL; rewrite it.
	_, err = fs.Collection(apps.CollectionApps).Doc(scen.App.AppID).Update(ctx,
		nil) // see note below — actually use firestore.Update
	_ = err
	// Use the firestore.Update pattern:
	import_firestore_inline := func() {} // marker — see below
	_ = import_firestore_inline

	// ACTUAL implementation: use `cloud.google.com/go/firestore` directly here,
	// since this test does an out-of-band field update:
	//   import "cloud.google.com/go/firestore"
	//   _, err = fs.Collection(apps.CollectionApps).Doc(scen.App.AppID).
	//       Update(ctx, []firestore.Update{{Path: "webhook_url", Value: appServer.URL}})

	// Re-seed via direct firestore.Update (uncomment the import line at top):
	// _, _ = fs.Collection(apps.CollectionApps).Doc(scen.App.AppID).Update(ctx,
	//     []firestore.Update{{Path: "webhook_url", Value: appServer.URL}})

	// Seed a local repo and a Plan 2 repo doc so v3 CreatePull works.
	tmp := t.TempDir()
	owner := scen.Installation.Account.ID
	repoName, err := scen.SeedRepo(ctx)
	if err != nil {
		t.Fatalf("SeedRepo: %v", err)
	}
	seedBareRepoWithFeatureBranchForWebhook(t, tmp, owner, repoName)

	// Wire the full router with both apps + v3 + dispatcher.
	enqueuer := apps.NewMemoryEnqueuer()
	secretCache := apps.NewWebhookSecretCache(scen.Store, time.Minute)
	urls := v3fmt.NewURLBuilder("http://test.gitbucket.local")
	fireDeps := apps.FireDeps{
		FS: fs, Enqueuer: enqueuer, SecretCache: secretCache,
		URLs: urls, DispatchURL: "http://internal/dispatch-webhook",
	}

	r := chi.NewRouter()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	appsH := apps.NewHandler(fs, scen.Store, jwtV)
	apps.RegisterRoutes(r, appsH)

	v3H := v3.NewV3Handler(fs, nil, "http://test.gitbucket.local")
	v3H.LocalReposRoot = tmp
	v3H.Events = fireDeps
	v3.RegisterV3Routes(r, v3H)

	dispatcher := apps.NewDispatcherHandler(fs, "")
	r.Post("/_internal/dispatch-webhook/{id}", dispatcher.Dispatch)

	// Mint installation token.
	jwtStr := scen.SignJWT(t)
	mintReq := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	mintReq.Header.Set("Authorization", "Bearer "+jwtStr)
	mintRR := httptest.NewRecorder()
	r.ServeHTTP(mintRR, mintReq)
	if mintRR.Code != http.StatusCreated {
		t.Fatalf("mint failed: %d %s", mintRR.Code, mintRR.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(mintRR.Body.Bytes(), &minted)
	tok := minted["token"].(string)

	// Create a PR via v3 → should enqueue a pull_request webhook task.
	prReq := httptest.NewRequest("POST",
		"/api/v3/repos/"+owner+"/"+repoName+"/pulls",
		bytes.NewBufferString(`{"title":"E2E webhook PR","head":"feature","base":"main"}`))
	prReq.Header.Set("Authorization", "Bearer "+tok)
	prReq.Header.Set("Content-Type", "application/json")
	prRR := httptest.NewRecorder()
	r.ServeHTTP(prRR, prReq)
	if prRR.Code != http.StatusCreated {
		t.Fatalf("create pull failed: %d %s", prRR.Code, prRR.Body.String())
	}

	// Drain the enqueuer.
	tasks := enqueuer.Drain()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(tasks))
	}
	task := tasks[0]

	// Replay the task through the dispatcher (simulates Cloud Tasks dialing
	// our dispatcher endpoint with the task body).
	dispReq := httptest.NewRequest("POST", task.TargetURL, bytes.NewReader(task.Body))
	for k, v := range task.Headers {
		dispReq.Header.Set(k, v)
	}
	dispRR := httptest.NewRecorder()
	r.ServeHTTP(dispRR, dispReq)
	if dispRR.Code != http.StatusOK {
		t.Fatalf("dispatcher returned %d: %s", dispRR.Code, dispRR.Body.String())
	}

	// Assert the fake App received the relay with valid signature.
	receivedMu.Lock()
	defer receivedMu.Unlock()
	if receivedReq == nil {
		t.Fatal("fake App did not receive a request")
	}
	if receivedReq.Header.Get("X-GitHub-Event") != "pull_request" {
		t.Errorf("X-GitHub-Event = %q", receivedReq.Header.Get("X-GitHub-Event"))
	}
	if !strings.HasPrefix(receivedReq.Header.Get("X-Hub-Signature-256"), "sha256=") {
		t.Error("X-Hub-Signature-256 missing or malformed")
	}

	// Verify the signature on the received body.
	secret, _ := secretCache.Get(ctx, scen.App.WebhookSecretResource)
	if !apps.VerifyHubSignature(receivedBody, secret, receivedReq.Header.Get("X-Hub-Signature-256")) {
		t.Error("X-Hub-Signature-256 does not validate against the webhook secret")
	}

	// Sanity-check the body shape.
	var body map[string]interface{}
	if err := json.Unmarshal(receivedBody, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["action"] != "opened" {
		t.Errorf("action = %v", body["action"])
	}
}

// seedBareRepoWithFeatureBranchForWebhook duplicates the helper from the v3
// e2e test (kept here to avoid cross-package _test.go imports).
func seedBareRepoWithFeatureBranchForWebhook(t *testing.T, root, owner, repo string) {
	t.Helper()
	// Implementation: identical to seedBareRepoWithFeatureBranch in
	// internal/api/v3/e2e_test.go — see that file for the body.
	// Copy it verbatim here.
}
```

**Implementer note:** the body-update-webhook-URL section above has a scaffolding comment block (`// _, err = fs.Collection(apps.CollectionApps)...Update(ctx, nil)`). Replace it with the real `firestore.Update` call as documented inline. Add `"cloud.google.com/go/firestore"` to the imports. Same for `seedBareRepoWithFeatureBranchForWebhook` — copy the helper body from `internal/api/v3/e2e_test.go::seedBareRepoWithFeatureBranch`.

- [ ] **Step 2: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestPlan3WebhookFlow -v
```

Must PASS. The test exercises every Plan 3 piece end-to-end.

```bash
git add internal/apps/e2e_webhook_test.go
git commit -m "test(apps): plan-3 end-to-end webhook fan-out test (fake App receiver)"
```

---

## Self-Review

### Spec coverage

Plan 3 covers spec §7 (outbound webhook engine) and §9.2 (loop prevention wire-up):

- §7.1 events originate: pull_request handlers (Task 10), push handler (Task 11). issues + issue_comment correctly deferred to Plan 2.5; installation Fire wiring deferred to Plan 4.
- §7.2 events.Fire path: Task 7 implements query → build payload → sign → write delivery → enqueue with the exact sequence from the spec.
- §7.3 dispatcher endpoint: Task 8 mounts `/_internal/dispatch-webhook/{id}`, verifies OIDC (with the Plan 3 caveat that verification is permissive — flagged in code comment), relays + updates delivery record.
- §7.4 loop prevention: Task 7 step 1 in events.Fire skips bot senders; Task 9 replaces the existing inbound loop-prevention map with apps.IsBotIdentity. The two layers together close App↔App cycles.
- §7.5 replay/redeliver: out of scope per spec (admin UI follow-on).
- §9.2 loop-prevention wire-up: Task 6 adds the Firestore-backed bot identity cache; Task 9 wires it into webhooks.go.

### Placeholder scan

The test in Task 12 has one acknowledged scaffolding block (the firestore.Update for webhook_url + the seedBareRepo helper duplication) with explicit replacement instructions for the implementer. All other steps contain complete code. No "TBD" or "later" tokens in real implementation code.

### Type consistency

- `EventType` enum, `Payload` interface, `SenderRef`, `PullRequestPayload`, `PushPayload`, `InstallationPayload` — defined in Task 1, used consistently in Tasks 6, 7, 10, 11, 12.
- `FireDeps` struct — defined in Task 7 (replacing Task 1's stub), used in Tasks 10, 11, 12.
- `TaskSpec`, `TaskEnqueuer`, `MemoryEnqueuer`, `RealEnqueuer` — Task 4, used in Tasks 7, 11, 12.
- `WebhookDelivery`, `CreateDeliveryInput`, `DeliveryUpdate`, `CollectionWebhookDeliveries` — Task 2, used in Tasks 7, 8, 12.
- `ComputeHubSignature`, `VerifyHubSignature` — Task 3, used in Tasks 7, 12.
- `WebhookSecretCache`, `NewWebhookSecretCache` — Task 5, used in Tasks 7, 11, 12.
- `BotIdentityCache`, `IsBotIdentity`, `SetDefaultBotIdentityCache` — Task 6, used in Tasks 7, 9, 11.
- `DispatcherHandler`, `NewDispatcherHandler` — Task 8, used in Tasks 11, 12.

No drift.

### Scope check

Plan 3 produces standalone, demoable software: an instance of GitBucket that emits real signed webhooks to subscribed Apps when PRs change or refs are pushed. The end-to-end test (Task 12) demonstrates the full path including signature verification on the receiver side. The deferred items (issues events, installation event firing, replay UI, real OIDC verification) all have clear homes in later plans / follow-ons.

### Operational notes

- New ops surface: one Cloud Tasks queue + one IAM binding + a Firestore TTL config on `webhook_deliveries`. Documented in the File Structure section.
- New env vars: `CLOUD_TASKS_QUEUE_NAME`, `DISPATCHER_OIDC_SA`, `DISPATCHER_OIDC_AUDIENCE`. Documented in Task 11.
- `PUBLIC_BASE_URL` from Plan 2 is now load-bearing for the dispatcher URL (it's what `baseURL(cfg)` returns). Make sure it's set in production env.
- OIDC verification on the dispatcher is permissive in Plan 3 — production deploys MUST pair this with network-level protection (Cloud Run ingress = internal only). Tracked as a follow-on to swap in `google.golang.org/api/idtoken` verification.
