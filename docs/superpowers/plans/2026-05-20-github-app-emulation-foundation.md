# GitHub App Emulation — Plan 1: Foundation (Data Model + Auth Plane)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the data model, Secret Manager-backed credential storage, JWT verification, installation-token issuance, and request-auth middleware that will eventually back the full `/api/v3/*` GitHub-compatible API. Plan 1 ships the four JWT-authed App endpoints (`GET /api/v3/app`, `GET /api/v3/app/installations`, `GET /api/v3/app/installations/{id}`, `POST /api/v3/app/installations/{id}/access_tokens`) plus a fully-tested installation-token middleware library — but no installation-token-authed endpoints yet (those land in Plan 2 alongside the GitHub-shape REST surface).

**Architecture:** New Go package `internal/apps/` mirroring `internal/auth/`. Firestore collections `apps`, `installations`, `installation_tokens` per spec §4. Private keys and webhook secrets live in Secret Manager; only resource names persist in Firestore. Token validation is O(1) by storing sha256(token) as the document ID. Two middleware layers (JWT and installation-token) mounted on disjoint `/api/v3/*` route groups.

**Tech Stack:** Go 1.x, chi v5 router, Firestore (`cloud.google.com/go/firestore v1.22`), Secret Manager (`cloud.google.com/go/secretmanager` — new dep), `github.com/golang-jwt/jwt/v4` (already indirect — promote to direct).

**Spec:** `docs/superpowers/specs/2026-05-20-github-app-emulation-design.md`

**Scope of Plan 1:** §3 Ownership Model, §4 Data Model, §5 Auth Plane, §9.1 Username regex change. Excludes §6 REST surface (Plan 2), §7 webhook engine (Plan 3), §8 manifest flow (Plan 4), §9.2 loop-prevention wire-up (Plan 3).

---

## File Structure

**New files:**
- `internal/apps/types.go` — Permission, AccountRef, App, Installation, InstallationToken Go types.
- `internal/apps/bots.go` — synthetic bot user creation; bot-username validator helper.
- `internal/apps/secrets.go` — `SecretStore` interface; `RealSecretStore` (Secret Manager) and `MemorySecretStore` (DEV_MODE / tests).
- `internal/apps/apps.go` — App data layer: CreateApp, GetApp, GetAppBySlug.
- `internal/apps/installations.go` — Installation data layer: CreateInstallation, GetInstallation, GetInstallationForApp, ListInstallationsForApp.
- `internal/apps/jwt.go` — RS256 JWT verification with per-app pubkey cache.
- `internal/apps/tokens.go` — MintInstallationToken, VerifyInstallationToken.
- `internal/apps/httpio.go` — WriteJSON, WriteError (GitHub-shape error body), error types.
- `internal/apps/jwt_middleware.go` — `RequireAppJWT` chi middleware.
- `internal/apps/middleware.go` — `RequireInstallationToken` chi middleware + `RequirePerm` helper.
- `internal/apps/handlers.go` — HTTP handlers for the 4 JWT-authed App endpoints.
- `internal/apps/routes.go` — `RegisterRoutes(r chi.Router, h *Handler)` to mount `/api/v3/app/*`.
- `internal/apps/testfixtures/fixtures.go` — `NewTestApp`, `NewTestInstallation`, `SignTestJWT` for use by tests in Plan 2 + Plan 3 too.
- `internal/apps/apps_test.go`, `installations_test.go`, `jwt_test.go`, `tokens_test.go`, `middleware_test.go`, `handlers_test.go`, `e2e_test.go` — Firestore-emulator integration tests.

**Modified files:**
- `internal/api/api.go` — username regex change (§9.1).
- `internal/config/config.go` — `SecretManagerProject` field (defaults to `ProjectID`).
- `main.go` — initialize `SecretStore`, construct `apps.Handler`, mount `/api/v3/app/*` routes.
- `go.mod`, `go.sum` — add `cloud.google.com/go/secretmanager`, promote `github.com/golang-jwt/jwt/v4` to direct.

**Manual ops (documented in Task 14):**
- `gcloud firestore fields ttls update expires_at --collection-group=installation_tokens --enable-ttl` (run once per environment).

---

## Task 1: Package scaffolding + core types

**Files:**
- Create: `internal/apps/types.go`
- Create: `internal/apps/types_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/types_test.go
package apps

import (
	"testing"
	"time"
)

func TestPermissionLevelString(t *testing.T) {
	cases := []struct {
		level PermissionLevel
		want  string
	}{
		{PermNone, "none"},
		{PermRead, "read"},
		{PermWrite, "write"},
	}
	for _, c := range cases {
		if got := c.level.String(); got != c.want {
			t.Errorf("PermissionLevel(%d).String() = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestPermissionsSatisfies(t *testing.T) {
	have := Permissions{"issues": PermWrite, "contents": PermRead}
	if !have.Satisfies("issues", PermWrite) {
		t.Error("write should satisfy write")
	}
	if !have.Satisfies("issues", PermRead) {
		t.Error("write should satisfy read")
	}
	if have.Satisfies("contents", PermWrite) {
		t.Error("read should not satisfy write")
	}
	if have.Satisfies("metadata", PermRead) {
		t.Error("missing permission should not satisfy")
	}
}

func TestAccountRefRoundTrip(t *testing.T) {
	a := AccountRef{ID: "uid-123", Type: AccountTypeUser}
	if a.Type != "User" {
		t.Errorf("AccountTypeUser = %q, want %q", a.Type, "User")
	}
}

func TestInstallationTokenExpired(t *testing.T) {
	tok := &InstallationToken{ExpiresAt: time.Now().Add(-time.Second)}
	if !tok.Expired(time.Now()) {
		t.Error("expected token to be expired")
	}
	tok.ExpiresAt = time.Now().Add(time.Hour)
	if tok.Expired(time.Now()) {
		t.Error("expected token not to be expired")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apps/...`
Expected: FAIL — package `apps` does not exist / undefined symbols.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/apps/types.go
// Package apps implements GitHub App emulation: registration, JWT verification,
// installation token issuance, request authentication, and webhook fan-out.
// Spec: docs/superpowers/specs/2026-05-20-github-app-emulation-design.md
package apps

import "time"

// PermissionLevel models GitHub App permission grants. Order matters: higher
// levels satisfy lower ones (Write satisfies Read).
type PermissionLevel int

const (
	PermNone PermissionLevel = iota
	PermRead
	PermWrite
)

func (p PermissionLevel) String() string {
	switch p {
	case PermWrite:
		return "write"
	case PermRead:
		return "read"
	default:
		return "none"
	}
}

func ParsePermissionLevel(s string) PermissionLevel {
	switch s {
	case "write":
		return PermWrite
	case "read":
		return PermRead
	default:
		return PermNone
	}
}

// Permissions is the map form used in JSON I/O and on Firestore docs.
type Permissions map[string]PermissionLevel

func (p Permissions) Satisfies(scope string, need PermissionLevel) bool {
	got, ok := p[scope]
	if !ok {
		return false
	}
	return got >= need
}

// AccountType discriminates the polymorphic account reference. MVP only emits
// "User"; "Organization" is reserved for a follow-on spec.
type AccountType string

const (
	AccountTypeUser AccountType = "User"
	AccountTypeOrg  AccountType = "Organization"
)

type AccountRef struct {
	ID   string      `firestore:"id" json:"id"`
	Type AccountType `firestore:"type" json:"type"`
}

// App is the Firestore-persisted representation of a registered GitHub App.
// See spec §4 for field meanings. Secret values themselves live in Secret
// Manager — only their resource names are stored here.
type App struct {
	AppID                 string      `firestore:"app_id"`
	Slug                  string      `firestore:"slug"`
	Name                  string      `firestore:"name"`
	OwnerAccount          AccountRef  `firestore:"owner_account"`
	BotUserID             string      `firestore:"bot_user_id"`
	ClientID              string      `firestore:"client_id"`
	ClientSecretHash      string      `firestore:"client_secret_hash"`
	WebhookURL            string      `firestore:"webhook_url"`
	WebhookSecretResource string      `firestore:"webhook_secret_resource"`
	WebhookSecretHash     string      `firestore:"webhook_secret_hash"`
	PrivateKeySecret      string      `firestore:"private_key_secret"`
	PublicKeyPEM          string      `firestore:"public_key_pem"`
	DefaultPermissions    Permissions `firestore:"default_permissions"`
	DefaultEvents         []string    `firestore:"default_events"`
	SuspendedAt           *time.Time  `firestore:"suspended_at"`
	CreatedAt             time.Time   `firestore:"created_at"`
}

// Installation links an App to an account (user, in MVP) and an optional
// subset of repository IDs.
type Installation struct {
	InstallationID      string      `firestore:"installation_id"`
	AppID               string      `firestore:"app_id"`
	Account             AccountRef  `firestore:"account"`
	RepositorySelection string      `firestore:"repository_selection"` // "all" | "selected"
	RepositoryIDs       []string    `firestore:"repository_ids"`
	Permissions         Permissions `firestore:"permissions"`
	Events              []string    `firestore:"events"`
	SuspendedAt         *time.Time  `firestore:"suspended_at"`
	CreatedAt           time.Time   `firestore:"created_at"`
}

// InstallationToken is the Firestore record for a minted access token. The
// document ID is the sha256 hex of the token plaintext — never the plaintext
// itself.
type InstallationToken struct {
	InstallationID string      `firestore:"installation_id"`
	Permissions    Permissions `firestore:"permissions"`
	RepositoryIDs  []string    `firestore:"repository_ids"`
	IssuedAt       time.Time   `firestore:"issued_at"`
	ExpiresAt      time.Time   `firestore:"expires_at"`
}

func (t *InstallationToken) Expired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

// Firestore collection names. Kept centralized so tests and seed scripts agree.
const (
	CollectionApps               = "apps"
	CollectionInstallations      = "installations"
	CollectionInstallationTokens = "installation_tokens"
	CollectionUsers              = "users" // existing collection; bot users live here too
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/apps/...`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/types.go internal/apps/types_test.go
git commit -m "feat(apps): add core types for App emulation (types, perms, accounts)"
```

---

## Task 2: Bot user model + username regex change

**Files:**
- Modify: `internal/api/api.go` — relax `usernameRegex` to allow `[bot]` suffix.
- Create: `internal/apps/bots.go` — bot username helpers + bot user creation.
- Create: `internal/apps/bots_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/bots_test.go
package apps

import (
	"context"
	"os"
	"testing"

	"cloud.google.com/go/firestore"
	"gitbucket/internal/db"
)

func TestBotUsernameValidator(t *testing.T) {
	cases := []struct {
		slug string
		want string
		ok   bool
	}{
		{"claude-code", "claude-code[bot]", true},
		{"a", "", false},          // too short slug
		{"x" + string(make([]byte, 21)), "", false}, // too long (>20)
		{"bad!", "", false},        // invalid chars
	}
	for _, c := range cases {
		got, err := BotUsernameForSlug(c.slug)
		if c.ok && err != nil {
			t.Errorf("BotUsernameForSlug(%q) unexpected error: %v", c.slug, err)
		}
		if !c.ok && err == nil {
			t.Errorf("BotUsernameForSlug(%q) expected error, got %q", c.slug, got)
		}
		if c.ok && got != c.want {
			t.Errorf("BotUsernameForSlug(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

func TestIsBotUsername(t *testing.T) {
	if !IsBotUsername("claude-code[bot]") {
		t.Error("claude-code[bot] should be a bot username")
	}
	if IsBotUsername("claude-code") {
		t.Error("claude-code should not be a bot username")
	}
}

func TestCreateBotUser(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()

	slug := "test-bot-" + randHex(4)
	uid, err := CreateBotUser(ctx, fs, slug, "Test Bot", "https://example.com/a.png", "app-xyz")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}
	if uid == "" {
		t.Fatal("expected non-empty uid")
	}

	doc, err := fs.Collection(CollectionUsers).Doc(uid).Get(ctx)
	if err != nil {
		t.Fatalf("read back bot user: %v", err)
	}
	data := doc.Data()
	if data["username"] != slug+"[bot]" {
		t.Errorf("username = %v, want %s[bot]", data["username"], slug)
	}
	if data["type"] != "Bot" {
		t.Errorf("type = %v, want Bot", data["type"])
	}
	if data["owning_app_id"] != "app-xyz" {
		t.Errorf("owning_app_id = %v, want app-xyz", data["owning_app_id"])
	}

	// cleanup
	_, _ = fs.Collection(CollectionUsers).Doc(uid).Delete(ctx)
	_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)

	_ = firestore.ServerTimestamp // silence unused import if reused
}
```

- [ ] **Step 2: Add the `randHex` test helper (used by many tests in this plan)**

Append to `internal/apps/bots_test.go` (or move into a shared `helpers_test.go` later):

```go
import (
	"crypto/rand"
	"encoding/hex"
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
```

Note: imports are usually grouped; merge with the imports block from Step 1 when actually editing.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/apps/...`
Expected: FAIL — undefined `BotUsernameForSlug`, `IsBotUsername`, `CreateBotUser`.

- [ ] **Step 4: Write the implementation**

```go
// internal/apps/bots.go
package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// slugRegex matches the slug part of a bot username (3-20 chars, alnum + hyphen).
// Same character set as the existing user-facing username regex; the `[bot]`
// suffix is appended by the App-registration path only.
var slugRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{3,20}$`)

// botUsernameRegex matches a full bot username, e.g. "claude-code[bot]".
var botUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{3,20}\[bot\]$`)

// BotUsernameForSlug returns "<slug>[bot]" if slug is valid, else an error.
func BotUsernameForSlug(slug string) (string, error) {
	if !slugRegex.MatchString(slug) {
		return "", fmt.Errorf("invalid slug %q (must match %s)", slug, slugRegex.String())
	}
	return slug + "[bot]", nil
}

// IsBotUsername returns true for usernames ending in `[bot]` and matching the
// reserved shape. Used by the user resolver to reject login / PAT writes for
// bot accounts.
func IsBotUsername(username string) bool {
	return botUsernameRegex.MatchString(username)
}

// CreateBotUser persists a synthetic bot user in Firestore and returns its UID.
// Also writes the usernames/{bot-username} → {uid} mapping so existing lookups
// (e.g. GetUidByUsername) work for bot logins.
func CreateBotUser(ctx context.Context, fs *firestore.Client, slug, displayName, avatarURL, owningAppID string) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	botUsername, err := BotUsernameForSlug(slug)
	if err != nil {
		return "", err
	}

	uidBytes := make([]byte, 16)
	if _, err := rand.Read(uidBytes); err != nil {
		return "", fmt.Errorf("generate bot uid: %w", err)
	}
	uid := "bot_" + hex.EncodeToString(uidBytes)

	usernameRef := fs.Collection("usernames").Doc(botUsername)
	userRef := fs.Collection(CollectionUsers).Doc(uid)

	err = fs.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Reject if the bot username is somehow taken.
		doc, err := tx.Get(usernameRef)
		if err == nil && doc.Exists() {
			return fmt.Errorf("bot username %q already taken", botUsername)
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		if err := tx.Set(usernameRef, map[string]interface{}{"uid": uid}); err != nil {
			return err
		}
		return tx.Set(userRef, map[string]interface{}{
			"uid":            uid,
			"username":       botUsername,
			"display_name":   displayName,
			"email":          fmt.Sprintf("%s@bots.gitbucket.local", strings.ToLower(botUsername)),
			"avatar_url":     avatarURL,
			"type":           "Bot",
			"owning_app_id":  owningAppID,
			"created_at":     time.Now().UTC(),
		})
	})
	if err != nil {
		return "", err
	}
	return uid, nil
}
```

- [ ] **Step 5: Update the user-facing username regex in `internal/api/api.go`**

Find the existing username regex (search for `usernameRegex` in the file). Change:

```go
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{3,20}$`)
```

to:

```go
// usernameRegex matches user-facing usernames. The `[bot]` suffix form is
// reserved for synthetic bot users owned by GitHub Apps and may only be
// created via the App registration path (see internal/apps/bots.go).
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{3,20}$`)
```

The regex itself does not change — user-facing signup continues to reject `[bot]`. Bots are created via `apps.CreateBotUser`, which bypasses this regex. Add a guard in the username-registration handler that explicitly rejects `IsBotUsername`-shaped strings as defense-in-depth.

Locate the existing `RegisterUsername` handler in `internal/api/api.go` (search for the `/api/user/username` route handler). At the top of the handler, after parsing the request body's `username` field, add:

```go
if apps.IsBotUsername(username) {
    http.Error(w, "username is reserved", http.StatusBadRequest)
    return
}
```

Add the import:

```go
import (
    // ...existing imports...
    "gitbucket/internal/apps"
)
```

- [ ] **Step 6: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestBotUsernameValidator -v
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestIsBotUsername -v
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateBotUser -v
go test ./internal/api/... -run TestAPIHandler -v
```

Expected: all PASS. (Existing API tests must still pass — the bot-username guard is additive and rejects only an explicitly-bot-shaped string that the original regex would have already rejected.)

- [ ] **Step 7: Commit**

```bash
git add internal/apps/bots.go internal/apps/bots_test.go internal/api/api.go
git commit -m "feat(apps): add bot user model and reserve [bot] suffix in username registration"
```

---

## Task 3: SecretStore abstraction

**Files:**
- Create: `internal/apps/secrets.go`
- Create: `internal/apps/secrets_test.go`

- [ ] **Step 1: Write the failing test (in-memory store only — Secret Manager has no offline tests)**

```go
// internal/apps/secrets_test.go
package apps

import (
	"context"
	"testing"
)

func TestMemorySecretStore_PutGetDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemorySecretStore()

	name, err := s.Put(ctx, "apps/test/key", []byte("hello"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if name == "" {
		t.Fatal("expected non-empty resource name")
	}

	got, err := s.Get(ctx, name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("Get returned %q, want %q", got, "hello")
	}

	if err := s.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, name); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestMemorySecretStore_GetUnknown(t *testing.T) {
	s := NewMemorySecretStore()
	if _, err := s.Get(context.Background(), "nope"); err == nil {
		t.Error("expected error for unknown resource")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apps/... -run TestMemorySecretStore -v`
Expected: FAIL — undefined `NewMemorySecretStore`.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/secrets.go
package apps

import (
	"context"
	"fmt"
	"sync"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// SecretStore abstracts secret persistence so production can use Secret Manager
// and dev / tests can use an in-memory store. Resource names are opaque strings
// from the store's perspective — for Secret Manager they look like
// `projects/<p>/secrets/<id>/versions/latest`; for memory store they are local
// keys.
type SecretStore interface {
	// Put stores plaintext under a logical name (e.g. "apps/{app_id}/private-key")
	// and returns the resource name to persist on the App row.
	Put(ctx context.Context, name string, plaintext []byte) (resourceName string, err error)
	// Get fetches the plaintext for a previously-stored resource name.
	Get(ctx context.Context, resourceName string) ([]byte, error)
	// Delete removes a secret. Returning nil on already-absent is acceptable.
	Delete(ctx context.Context, resourceName string) error
}

// --- Memory implementation -------------------------------------------------

type MemorySecretStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{m: make(map[string][]byte)}
}

func (s *MemorySecretStore) Put(_ context.Context, name string, plaintext []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(plaintext))
	copy(cp, plaintext)
	s.m[name] = cp
	return "memory://" + name, nil
}

func (s *MemorySecretStore) Get(_ context.Context, resourceName string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := resourceName
	if len(resourceName) > len("memory://") && resourceName[:len("memory://")] == "memory://" {
		key = resourceName[len("memory://"):]
	}
	v, ok := s.m[key]
	if !ok {
		return nil, fmt.Errorf("secret not found: %s", resourceName)
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (s *MemorySecretStore) Delete(_ context.Context, resourceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceName
	if len(resourceName) > len("memory://") && resourceName[:len("memory://")] == "memory://" {
		key = resourceName[len("memory://"):]
	}
	delete(s.m, key)
	return nil
}

// --- Secret Manager implementation -----------------------------------------

type RealSecretStore struct {
	client    *secretmanager.Client
	projectID string
}

func NewRealSecretStore(client *secretmanager.Client, projectID string) *RealSecretStore {
	return &RealSecretStore{client: client, projectID: projectID}
}

func (s *RealSecretStore) Put(ctx context.Context, name string, plaintext []byte) (string, error) {
	secretID := sanitizeSecretID(name)
	parent := fmt.Sprintf("projects/%s", s.projectID)

	// Create the Secret container if missing; tolerate AlreadyExists.
	_, err := s.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   parent,
		SecretId: secretID,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
		},
	})
	if err != nil && !isAlreadyExists(err) {
		return "", fmt.Errorf("create secret %s: %w", secretID, err)
	}

	// Add a new version with the plaintext.
	versionResp, err := s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  fmt.Sprintf("%s/secrets/%s", parent, secretID),
		Payload: &secretmanagerpb.SecretPayload{Data: plaintext},
	})
	if err != nil {
		return "", fmt.Errorf("add secret version: %w", err)
	}
	return versionResp.Name, nil
}

func (s *RealSecretStore) Get(ctx context.Context, resourceName string) ([]byte, error) {
	resp, err := s.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: resourceName,
	})
	if err != nil {
		return nil, fmt.Errorf("access secret %s: %w", resourceName, err)
	}
	return resp.Payload.Data, nil
}

func (s *RealSecretStore) Delete(ctx context.Context, resourceName string) error {
	// resourceName for a version is .../secrets/<id>/versions/<n>. To delete the
	// whole secret we trim back to .../secrets/<id>.
	parent := trimVersion(resourceName)
	err := s.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: parent})
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

func sanitizeSecretID(name string) string {
	// Secret Manager allows [A-Za-z0-9_-]; replace slashes from logical names.
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

func trimVersion(s string) string {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == '/' {
			// Look for "/versions/" boundary.
			if i >= len("/versions/") && s[i-len("/versions/")+1:i+1] == "/versions/" {
				return s[:i-len("/versions/")+1]
			}
		}
	}
	return s
}

func isAlreadyExists(err error) bool {
	return err != nil && (containsCode(err, "AlreadyExists") || containsCode(err, "already exists"))
}
func isNotFound(err error) bool {
	return err != nil && (containsCode(err, "NotFound") || containsCode(err, "not found"))
}
func containsCode(err error, needle string) bool {
	s := err.Error()
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add Secret Manager dependency**

```bash
go get cloud.google.com/go/secretmanager@latest
go mod tidy
```

- [ ] **Step 5: Run tests to verify the in-memory store passes; the real store has no offline tests**

```bash
go test ./internal/apps/... -run TestMemorySecretStore -v
```

Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/apps/secrets.go internal/apps/secrets_test.go go.mod go.sum
git commit -m "feat(apps): add SecretStore abstraction (Memory + Secret Manager impls)"
```

---

## Task 4: App data layer

**Files:**
- Create: `internal/apps/apps.go`
- Create: `internal/apps/apps_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/apps_test.go
package apps

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"gitbucket/internal/db"
)

func TestCreateAndGetApp(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	store := NewMemorySecretStore()
	slug := "test-app-" + randHex(4)
	owner := AccountRef{ID: "owner-uid-" + randHex(2), Type: AccountTypeUser}
	botUID, err := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}

	req := CreateAppRequest{
		Slug:               slug,
		Name:               "Test App",
		OwnerAccount:       owner,
		BotUserID:          botUID,
		WebhookURL:         "https://example.com/hook",
		DefaultPermissions: Permissions{"issues": PermWrite, "metadata": PermRead},
		DefaultEvents:      []string{"issues", "issue_comment"},
	}
	created, secrets, err := CreateApp(ctx, fs, store, req)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.AppID == "" {
		t.Fatal("AppID should be set")
	}
	if secrets.ClientSecret == "" || secrets.WebhookSecret == "" || secrets.PrivateKeyPEM == "" {
		t.Fatal("secrets should be populated exactly once at creation")
	}

	// PEM should parse as a valid RSA private key.
	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	if block == nil {
		t.Fatal("PrivateKeyPEM did not decode")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse pkcs1: %v", err)
	}
	if _, ok := any(key).(*rsa.PrivateKey); !ok {
		t.Fatal("parsed key is not *rsa.PrivateKey")
	}

	// Read back via GetApp.
	got, err := GetApp(ctx, fs, created.AppID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.Slug != slug || got.Name != "Test App" {
		t.Errorf("GetApp mismatch: %+v", got)
	}
	if got.PublicKeyPEM == "" {
		t.Error("PublicKeyPEM should be persisted")
	}

	// GetAppBySlug works too.
	bySlug, err := GetAppBySlug(ctx, fs, slug)
	if err != nil {
		t.Fatalf("GetAppBySlug: %v", err)
	}
	if bySlug.AppID != created.AppID {
		t.Errorf("GetAppBySlug returned %q, want %q", bySlug.AppID, created.AppID)
	}

	// Cleanup.
	_, _ = fs.Collection(CollectionApps).Doc(created.AppID).Delete(ctx)
	_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
	_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
}

func TestCreateApp_DuplicateSlug(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()
	slug := "dup-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}

	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	req := CreateAppRequest{Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL: "https://example.com/x"}
	if _, _, err := CreateApp(ctx, fs, store, req); err != nil {
		t.Fatalf("first CreateApp: %v", err)
	}
	if _, _, err := CreateApp(ctx, fs, store, req); err == nil {
		t.Error("expected duplicate slug to fail")
	}

	_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
	_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
	docs, _ := fs.Collection(CollectionApps).Where("slug", "==", slug).Documents(ctx).GetAll()
	for _, d := range docs {
		_, _ = d.Ref.Delete(ctx)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateAndGetApp -v`
Expected: FAIL — undefined `CreateApp`, `GetApp`, `GetAppBySlug`, `CreateAppRequest`.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/apps.go
package apps

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// CreateAppRequest is the input to CreateApp. All fields are required except
// DefaultEvents (which falls back to a built-in baseline).
type CreateAppRequest struct {
	Slug               string
	Name               string
	OwnerAccount       AccountRef
	BotUserID          string
	WebhookURL         string
	DefaultPermissions Permissions
	DefaultEvents      []string
}

// AppSecrets carries the plaintext secrets returned exactly once from CreateApp.
// Callers MUST relay these to the registering App and then drop the references.
type AppSecrets struct {
	ClientID       string
	ClientSecret   string
	WebhookSecret  string
	PrivateKeyPEM  string
}

var defaultEventsBaseline = []string{
	"issues", "issue_comment", "pull_request", "pull_request_review_comment", "push", "installation",
}

// CreateApp generates RSA-2048 keypair, persists private key + webhook secret to
// SecretStore, writes the apps/{app_id} document, and returns the App + the
// one-time plaintext secrets bundle.
func CreateApp(ctx context.Context, fs *firestore.Client, store SecretStore, req CreateAppRequest) (*App, *AppSecrets, error) {
	if fs == nil {
		return nil, nil, fmt.Errorf("firestore client is nil")
	}
	if store == nil {
		return nil, nil, fmt.Errorf("secret store is nil")
	}
	if req.Slug == "" || req.Name == "" || req.OwnerAccount.ID == "" || req.BotUserID == "" || req.WebhookURL == "" {
		return nil, nil, fmt.Errorf("missing required CreateAppRequest field")
	}

	// Uniqueness pre-check: reject if slug is already taken. The final write
	// also runs in a transaction that re-checks, but pre-checking avoids
	// generating an RSA key for nothing.
	existing, err := GetAppBySlug(ctx, fs, req.Slug)
	if err != nil {
		return nil, nil, fmt.Errorf("slug uniqueness check: %w", err)
	}
	if existing != nil {
		return nil, nil, fmt.Errorf("app slug %q already taken", req.Slug)
	}

	appIDBytes := make([]byte, 16)
	if _, err := rand.Read(appIDBytes); err != nil {
		return nil, nil, fmt.Errorf("generate app_id: %w", err)
	}
	appID := hex.EncodeToString(appIDBytes)

	clientIDBytes := make([]byte, 12)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return nil, nil, fmt.Errorf("generate client_id: %w", err)
	}
	clientID := "Iv1." + hex.EncodeToString(clientIDBytes)

	clientSecret, err := randomToken(32)
	if err != nil {
		return nil, nil, fmt.Errorf("generate client_secret: %w", err)
	}
	webhookSecret, err := randomToken(32)
	if err != nil {
		return nil, nil, fmt.Errorf("generate webhook_secret: %w", err)
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate rsa key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal pubkey: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	privResource, err := store.Put(ctx, fmt.Sprintf("apps/%s/private-key", appID), privPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("store private key: %w", err)
	}
	webhookResource, err := store.Put(ctx, fmt.Sprintf("apps/%s/webhook-secret", appID), []byte(webhookSecret))
	if err != nil {
		// Best-effort rollback.
		_ = store.Delete(ctx, privResource)
		return nil, nil, fmt.Errorf("store webhook secret: %w", err)
	}

	defaultEvents := req.DefaultEvents
	if len(defaultEvents) == 0 {
		defaultEvents = defaultEventsBaseline
	}

	now := time.Now().UTC()
	app := &App{
		AppID:                 appID,
		Slug:                  req.Slug,
		Name:                  req.Name,
		OwnerAccount:          req.OwnerAccount,
		BotUserID:             req.BotUserID,
		ClientID:              clientID,
		ClientSecretHash:      sha256Hex(clientSecret),
		WebhookURL:            req.WebhookURL,
		WebhookSecretResource: webhookResource,
		WebhookSecretHash:     sha256Hex(webhookSecret),
		PrivateKeySecret:      privResource,
		PublicKeyPEM:          string(pubPEM),
		DefaultPermissions:    req.DefaultPermissions,
		DefaultEvents:         defaultEvents,
		SuspendedAt:           nil,
		CreatedAt:             now,
	}

	appRef := fs.Collection(CollectionApps).Doc(appID)
	if _, err := appRef.Create(ctx, app); err != nil {
		_ = store.Delete(ctx, privResource)
		_ = store.Delete(ctx, webhookResource)
		return nil, nil, fmt.Errorf("write app doc: %w", err)
	}

	return app, &AppSecrets{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		WebhookSecret: webhookSecret,
		PrivateKeyPEM: string(privPEM),
	}, nil
}

// GetApp loads an App by its app_id. Returns (nil, nil) if not found.
func GetApp(ctx context.Context, fs *firestore.Client, appID string) (*App, error) {
	doc, err := fs.Collection(CollectionApps).Doc(appID).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var a App
	if err := doc.DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAppBySlug returns (nil, nil) if no App with this slug exists.
func GetAppBySlug(ctx context.Context, fs *firestore.Client, slug string) (*App, error) {
	iter := fs.Collection(CollectionApps).Where("slug", "==", slug).Limit(1).Documents(ctx)
	defer iter.Stop()
	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a App
	if err := doc.DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// --- internal helpers ------------------------------------------------------

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isFirestoreNotFound(err error) bool {
	if err == nil {
		return false
	}
	// google.golang.org/grpc/codes is imported elsewhere in this package; use a
	// substring check to avoid pulling it in just for this helper.
	s := err.Error()
	return contains(s, "NotFound") || contains(s, "not found")
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run "TestCreateAndGetApp|TestCreateApp_DuplicateSlug" -v
```

Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/apps.go internal/apps/apps_test.go
git commit -m "feat(apps): add App data layer (CreateApp / GetApp / GetAppBySlug)"
```

---

## Task 5: Installation data layer

**Files:**
- Create: `internal/apps/installations.go`
- Create: `internal/apps/installations_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/installations_test.go
package apps

import (
	"context"
	"os"
	"testing"

	"gitbucket/internal/db"
)

func TestCreateAndGetInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "inst-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, _, err := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL: "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite},
		DefaultEvents:      []string{"issues"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, err := CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID:               app.AppID,
		Account:             installeeAcct,
		RepositorySelection: "selected",
		RepositoryIDs:       []string{"repo1", "repo2"},
		Permissions:         Permissions{"issues": PermWrite},
		Events:              []string{"issues"},
	})
	if err != nil {
		t.Fatalf("CreateInstallation: %v", err)
	}
	if inst.InstallationID == "" {
		t.Fatal("InstallationID should be set")
	}

	got, err := GetInstallation(ctx, fs, inst.InstallationID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if got.AppID != app.AppID {
		t.Errorf("AppID = %s, want %s", got.AppID, app.AppID)
	}
	if len(got.RepositoryIDs) != 2 {
		t.Errorf("RepositoryIDs len = %d, want 2", len(got.RepositoryIDs))
	}

	// GetInstallationForApp enforces ownership.
	got2, err := GetInstallationForApp(ctx, fs, inst.InstallationID, app.AppID)
	if err != nil || got2 == nil {
		t.Fatalf("GetInstallationForApp same app: err=%v inst=%v", err, got2)
	}
	got3, err := GetInstallationForApp(ctx, fs, inst.InstallationID, "wrong-app-id")
	if err != nil {
		t.Fatalf("GetInstallationForApp wrong app err: %v", err)
	}
	if got3 != nil {
		t.Error("GetInstallationForApp should return nil for wrong app")
	}

	// ListInstallationsForApp finds it.
	list, err := ListInstallationsForApp(ctx, fs, app.AppID)
	if err != nil {
		t.Fatalf("ListInstallationsForApp: %v", err)
	}
	if len(list) != 1 || list[0].InstallationID != inst.InstallationID {
		t.Errorf("list = %+v", list)
	}

	// Cleanup.
	_, _ = fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(ctx)
	_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx)
	_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
	_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateAndGetInstallation -v`
Expected: FAIL — undefined `CreateInstallation`, etc.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/installations.go
package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type CreateInstallationRequest struct {
	AppID               string
	Account             AccountRef
	RepositorySelection string
	RepositoryIDs       []string
	Permissions         Permissions
	Events              []string
}

func CreateInstallation(ctx context.Context, fs *firestore.Client, req CreateInstallationRequest) (*Installation, error) {
	if fs == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	if req.AppID == "" || req.Account.ID == "" {
		return nil, fmt.Errorf("AppID and Account.ID are required")
	}
	if req.RepositorySelection != "all" && req.RepositorySelection != "selected" {
		return nil, fmt.Errorf("repository_selection must be 'all' or 'selected', got %q", req.RepositorySelection)
	}
	if req.RepositorySelection == "selected" && len(req.RepositoryIDs) == 0 {
		return nil, fmt.Errorf("repository_ids required when repository_selection is 'selected'")
	}

	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate installation_id: %w", err)
	}
	id := hex.EncodeToString(idBytes)

	inst := &Installation{
		InstallationID:      id,
		AppID:               req.AppID,
		Account:             req.Account,
		RepositorySelection: req.RepositorySelection,
		RepositoryIDs:       req.RepositoryIDs,
		Permissions:         req.Permissions,
		Events:              req.Events,
		CreatedAt:           time.Now().UTC(),
	}
	if _, err := fs.Collection(CollectionInstallations).Doc(id).Create(ctx, inst); err != nil {
		return nil, fmt.Errorf("write installation: %w", err)
	}
	return inst, nil
}

// GetInstallation returns (nil, nil) if not found.
func GetInstallation(ctx context.Context, fs *firestore.Client, id string) (*Installation, error) {
	doc, err := fs.Collection(CollectionInstallations).Doc(id).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var i Installation
	if err := doc.DataTo(&i); err != nil {
		return nil, err
	}
	return &i, nil
}

// GetInstallationForApp returns the installation only if it belongs to appID.
// Returns (nil, nil) when not found OR not owned by that App — callers should
// not distinguish these to avoid leaking existence.
func GetInstallationForApp(ctx context.Context, fs *firestore.Client, id, appID string) (*Installation, error) {
	inst, err := GetInstallation(ctx, fs, id)
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.AppID != appID {
		return nil, nil
	}
	return inst, nil
}

func ListInstallationsForApp(ctx context.Context, fs *firestore.Client, appID string) ([]*Installation, error) {
	var out []*Installation
	iter := fs.Collection(CollectionInstallations).Where("app_id", "==", appID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var i Installation
		if err := doc.DataTo(&i); err != nil {
			return nil, err
		}
		out = append(out, &i)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateAndGetInstallation -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/installations.go internal/apps/installations_test.go
git commit -m "feat(apps): add Installation data layer"
```

---

## Task 6: JWT verification

**Files:**
- Create: `internal/apps/jwt.go`
- Create: `internal/apps/jwt_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/jwt_test.go
package apps

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"gitbucket/internal/db"
)

func signTestJWT(t *testing.T, appID string, key *rsa.PrivateKey, iat, exp time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": appID,
		"iat": iat.Unix(),
		"exp": exp.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

func setupTestApp(ctx context.Context, t *testing.T, fs interface{ /* placeholder; see real */ }) {
	// see TestVerifyAppJWT for the real bootstrap
}

func TestVerifyAppJWT(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "jwt-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, secrets, err := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	defer func() {
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
	}()

	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	priv, _ := x509.ParsePKCS1PrivateKey(block.Bytes)

	verifier := NewJWTVerifier(fs, 60*time.Second)

	t.Run("valid", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now, now.Add(5*time.Minute))
		gotApp, err := verifier.Verify(ctx, tok)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if gotApp.AppID != app.AppID {
			t.Errorf("returned app_id = %s, want %s", gotApp.AppID, app.AppID)
		}
	})

	t.Run("expired", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now.Add(-10*time.Minute), now.Add(-5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for expired token")
		}
	})

	t.Run("exp window too wide", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now, now.Add(20*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for exp >10m after iat")
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		other, _ := rsa.GenerateKey(rand.Reader, 2048)
		now := time.Now()
		tok := signTestJWT(t, app.AppID, other, now, now.Add(5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected signature error for wrong key")
		}
	})

	t.Run("unknown issuer", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, "no-such-app", priv, now, now.Add(5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for unknown issuer")
		}
	})

	t.Run("suspended app", func(t *testing.T) {
		// Suspend the app.
		now := time.Now().UTC()
		_, err := fs.Collection(CollectionApps).Doc(app.AppID).Update(ctx,
			[]firestoreUpdate{{Path: "suspended_at", Value: now}}.toFirestore())
		if err != nil {
			t.Skipf("could not suspend app (helper not yet wired): %v", err)
		}
		verifier.InvalidateCache(app.AppID)
		tok := signTestJWT(t, app.AppID, priv, time.Now(), time.Now().Add(5*time.Minute))
		if _, err := verifier.Verify(ctx, tok); err == nil {
			t.Error("expected error for suspended app")
		}
		// Un-suspend so other tests can re-use.
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Update(ctx,
			[]firestoreUpdate{{Path: "suspended_at", Value: nil}}.toFirestore())
		verifier.InvalidateCache(app.AppID)
	})
}

// firestoreUpdate is a tiny adapter so this test file does not import
// cloud.google.com/go/firestore just for the Update type.
type firestoreUpdate struct {
	Path  string
	Value interface{}
}

type firestoreUpdateList []firestoreUpdate

func (l firestoreUpdateList) toFirestore() []firestoreUpdateRaw {
	out := make([]firestoreUpdateRaw, len(l))
	for i, u := range l {
		out[i] = firestoreUpdateRaw{Path: u.Path, Value: u.Value}
	}
	return out
}

// firestoreUpdateRaw matches the in-package `firestore.Update` shape but is
// satisfied via interface conversion when DataTo accepts it. This is a hack to
// keep the test free of the firestore import — REPLACE this scaffolding with a
// direct firestore.Update import in actual code; see real implementation below.
type firestoreUpdateRaw = struct {
	Path  string
	Value interface{}
}
```

**Note for implementer:** the test above has a scaffolding hack (`firestoreUpdate`) to avoid importing `firestore` in the test file. When you actually write the test, replace it with a direct import:

```go
import "cloud.google.com/go/firestore"

// ... and use:
_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Update(ctx, []firestore.Update{
    {Path: "suspended_at", Value: time.Now().UTC()},
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestVerifyAppJWT -v`
Expected: FAIL — undefined `NewJWTVerifier`.

- [ ] **Step 3: Promote `golang-jwt/jwt/v4` to a direct dependency**

```bash
go get github.com/golang-jwt/jwt/v4@latest
go mod tidy
```

- [ ] **Step 4: Write the implementation**

```go
// internal/apps/jwt.go
package apps

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/golang-jwt/jwt/v4"
)

// JWTVerifier verifies App-signed JWTs against per-app public keys cached
// in-process. Cache TTL is enforced via the cacheTTL passed to NewJWTVerifier.
// Spec §5.1.
type JWTVerifier struct {
	fs       *firestore.Client
	cacheTTL time.Duration

	mu    sync.RWMutex
	cache map[string]jwtCacheEntry
}

type jwtCacheEntry struct {
	app       *App
	pubKey    *rsa.PublicKey
	expiresAt time.Time
}

const (
	jwtClockSkew      = 30 * time.Second
	jwtMaxExpWindow   = 10 * time.Minute
)

func NewJWTVerifier(fs *firestore.Client, cacheTTL time.Duration) *JWTVerifier {
	return &JWTVerifier{
		fs:       fs,
		cacheTTL: cacheTTL,
		cache:    make(map[string]jwtCacheEntry),
	}
}

// Verify parses tokenStr, looks up the issuing App, verifies the signature and
// time-bound claims, and returns the App on success. Errors are intentionally
// non-specific (no leaks about which check failed) and all map to 401 at the
// HTTP layer.
func (v *JWTVerifier) Verify(ctx context.Context, tokenStr string) (*App, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Name}))
	// First parse claims without verifying signature to extract `iss`.
	parsed, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("invalid jwt")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid jwt claims")
	}
	iss, _ := claims["iss"].(string)
	if iss == "" {
		return nil, fmt.Errorf("missing iss")
	}

	entry, err := v.loadEntry(ctx, iss)
	if err != nil || entry == nil {
		return nil, fmt.Errorf("invalid jwt")
	}
	if entry.app.SuspendedAt != nil {
		return nil, fmt.Errorf("app suspended")
	}

	// Now verify signature with the cached pubkey.
	_, err = parser.Parse(tokenStr, func(_ *jwt.Token) (interface{}, error) {
		return entry.pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid jwt signature")
	}

	now := time.Now()
	iatF, _ := claims["iat"].(float64)
	expF, _ := claims["exp"].(float64)
	if iatF == 0 || expF == 0 {
		return nil, fmt.Errorf("missing iat/exp")
	}
	iat := time.Unix(int64(iatF), 0)
	exp := time.Unix(int64(expF), 0)

	if iat.After(now.Add(jwtClockSkew)) {
		return nil, fmt.Errorf("iat in future")
	}
	if exp.Before(now.Add(-jwtClockSkew)) {
		return nil, fmt.Errorf("exp in past")
	}
	if exp.Sub(iat) > jwtMaxExpWindow+jwtClockSkew {
		return nil, fmt.Errorf("exp window too wide")
	}
	return entry.app, nil
}

func (v *JWTVerifier) InvalidateCache(appID string) {
	v.mu.Lock()
	delete(v.cache, appID)
	v.mu.Unlock()
}

func (v *JWTVerifier) loadEntry(ctx context.Context, appID string) (*jwtCacheEntry, error) {
	v.mu.RLock()
	e, ok := v.cache[appID]
	v.mu.RUnlock()
	if ok && time.Now().Before(e.expiresAt) {
		return &e, nil
	}

	app, err := GetApp(ctx, v.fs, appID)
	if err != nil || app == nil {
		return nil, err
	}
	pubKey, err := parseRSAPublicKey(app.PublicKeyPEM)
	if err != nil {
		return nil, err
	}
	entry := jwtCacheEntry{
		app:       app,
		pubKey:    pubKey,
		expiresAt: time.Now().Add(v.cacheTTL),
	}
	v.mu.Lock()
	v.cache[appID] = entry
	v.mu.Unlock()
	return &entry, nil
}

func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("decode pem")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an rsa public key")
	}
	return rsaPub, nil
}
```

- [ ] **Step 5: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestVerifyAppJWT -v
```

Expected: all subtests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/jwt.go internal/apps/jwt_test.go go.mod go.sum
git commit -m "feat(apps): verify App-signed RS256 JWTs with per-app pubkey cache"
```

---

## Task 7: Installation token minting & verification

**Files:**
- Create: `internal/apps/tokens.go`
- Create: `internal/apps/tokens_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/tokens_test.go
package apps

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestMintAndVerifyInstallationToken(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "tok-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, _, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite, "contents": PermWrite},
		DefaultEvents:      []string{"issues"},
	})
	defer func() {
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
	}()

	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         Permissions{"issues": PermWrite, "contents": PermWrite},
		Events:              []string{"issues"},
	})
	defer fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(ctx)

	t.Run("default mint", func(t *testing.T) {
		out, err := MintInstallationToken(ctx, fs, inst, MintRequest{})
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if !strings.HasPrefix(out.Plaintext, "ghs_") {
			t.Errorf("token prefix = %q, want ghs_", out.Plaintext[:4])
		}
		// 1-hour expiry, within 5 seconds.
		want := time.Now().Add(time.Hour)
		diff := out.Record.ExpiresAt.Sub(want)
		if diff < -5*time.Second || diff > 5*time.Second {
			t.Errorf("expires_at off by %s", diff)
		}
		// Should be readable back via VerifyInstallationToken.
		v, err := VerifyInstallationToken(ctx, fs, out.Plaintext)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if v.InstallationID != inst.InstallationID {
			t.Errorf("verify returned installation %s, want %s", v.InstallationID, inst.InstallationID)
		}
	})

	t.Run("subset perms ok", func(t *testing.T) {
		out, err := MintInstallationToken(ctx, fs, inst, MintRequest{
			Permissions: Permissions{"issues": PermRead},
		})
		if err != nil {
			t.Fatalf("mint subset: %v", err)
		}
		if out.Record.Permissions["issues"] != PermRead {
			t.Errorf("perms = %v", out.Record.Permissions)
		}
	})

	t.Run("broader perms rejected", func(t *testing.T) {
		_, err := MintInstallationToken(ctx, fs, inst, MintRequest{
			Permissions: Permissions{"workflows": PermWrite},
		})
		if err == nil {
			t.Error("expected error for permission not granted to installation")
		}
	})

	t.Run("verify rejects expired token", func(t *testing.T) {
		out, _ := MintInstallationToken(ctx, fs, inst, MintRequest{})
		// Manually backdate expires_at.
		hash := sha256Hex(out.Plaintext)
		_, _ = fs.Collection(CollectionInstallationTokens).Doc(hash).Update(ctx,
			[]firestoreUpdateRaw{}, // placeholder; real impl uses firestore.Update
		)
		// see note below for the real call shape.
	})

	t.Run("verify rejects unknown token", func(t *testing.T) {
		_, err := VerifyInstallationToken(ctx, fs, "ghs_definitely-not-real")
		if err == nil {
			t.Error("expected error for unknown token")
		}
	})
}
```

**Implementer note:** the "verify rejects expired token" subtest uses `firestoreUpdateRaw` as a placeholder to keep the test file ergonomic in the plan. In the actual implementation, replace it with:

```go
import "cloud.google.com/go/firestore"

_, _ = fs.Collection(CollectionInstallationTokens).Doc(hash).Update(ctx, []firestore.Update{
    {Path: "expires_at", Value: time.Now().Add(-time.Minute)},
})
if _, err := VerifyInstallationToken(ctx, fs, out.Plaintext); err == nil {
    t.Error("expected expired token to fail verification")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestMintAndVerifyInstallationToken -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/tokens.go
package apps

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

const (
	tokenPrefix    = "ghs_"
	tokenEntropyN  = 24 // 24 bytes → 39 base32 chars; total token len ~43
	TokenTTL       = time.Hour
)

// MintRequest carries optional narrowing of the issued token's scope.
type MintRequest struct {
	Permissions   Permissions
	RepositoryIDs []string
}

type MintOutput struct {
	Plaintext string             // returned exactly once
	Record    *InstallationToken // the persisted record (without the plaintext)
}

// MintInstallationToken validates that the requested permissions / repo subset
// are within the installation's grant, generates a ghs_-prefixed token,
// persists the sha256(token) as the document ID, and returns the plaintext.
func MintInstallationToken(ctx context.Context, fs *firestore.Client, inst *Installation, req MintRequest) (*MintOutput, error) {
	if inst == nil {
		return nil, fmt.Errorf("nil installation")
	}
	perms := req.Permissions
	if len(perms) == 0 {
		perms = inst.Permissions
	} else {
		for scope, need := range perms {
			if !inst.Permissions.Satisfies(scope, need) {
				return nil, fmt.Errorf("requested permission %s:%s not granted to installation", scope, need.String())
			}
		}
	}

	repoIDs := req.RepositoryIDs
	if len(repoIDs) == 0 {
		repoIDs = inst.RepositoryIDs
	} else if inst.RepositorySelection == "selected" {
		allowed := map[string]bool{}
		for _, id := range inst.RepositoryIDs {
			allowed[id] = true
		}
		for _, id := range repoIDs {
			if !allowed[id] {
				return nil, fmt.Errorf("repository %s not granted to installation", id)
			}
		}
	}

	plaintext, err := generateGHSToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	now := time.Now().UTC()
	rec := &InstallationToken{
		InstallationID: inst.InstallationID,
		Permissions:    perms,
		RepositoryIDs:  repoIDs,
		IssuedAt:       now,
		ExpiresAt:      now.Add(TokenTTL),
	}
	hash := sha256Hex(plaintext)
	if _, err := fs.Collection(CollectionInstallationTokens).Doc(hash).Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("persist token: %w", err)
	}
	return &MintOutput{Plaintext: plaintext, Record: rec}, nil
}

// VerifyInstallationToken hashes the inbound token, point-reads the record,
// and rejects missing or expired tokens. Expiry is checked explicitly here —
// Firestore TTL is a sweep, not real-time.
func VerifyInstallationToken(ctx context.Context, fs *firestore.Client, plaintext string) (*InstallationToken, error) {
	if !strings.HasPrefix(plaintext, tokenPrefix) {
		return nil, fmt.Errorf("invalid token format")
	}
	hash := sha256Hex(plaintext)
	doc, err := fs.Collection(CollectionInstallationTokens).Doc(hash).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, fmt.Errorf("token not found")
		}
		return nil, err
	}
	var t InstallationToken
	if err := doc.DataTo(&t); err != nil {
		return nil, err
	}
	if t.Expired(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}
	return &t, nil
}

// --- helpers ---------------------------------------------------------------

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

func generateGHSToken() (string, error) {
	b := make([]byte, tokenEntropyN)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return tokenPrefix + strings.ToLower(base32NoPad.EncodeToString(b)), nil
}
```

- [ ] **Step 4: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestMintAndVerifyInstallationToken -v
```

Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/tokens.go internal/apps/tokens_test.go
git commit -m "feat(apps): mint and verify installation tokens (ghs_, sha256 doc id)"
```

---

## Task 8: HTTP error & JSON helpers (GitHub-shape responses)

**Files:**
- Create: `internal/apps/httpio.go`
- Create: `internal/apps/httpio_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/httpio_test.go
package apps

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteJSONSetsContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, 200, map[string]string{"hello": "world"})
	if got := rr.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if rr.Code != 200 {
		t.Errorf("code = %d", rr.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["hello"] != "world" {
		t.Errorf("body = %v", got)
	}
}

func TestWriteErrorGitHubShape(t *testing.T) {
	cases := []struct {
		err     error
		wantCode int
		wantMsg  string
	}{
		{ErrUnauthorized, 401, "Bad credentials"},
		{ErrNotFound, 404, "Not Found"},
		{ErrForbidden, 403, "Forbidden"},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		WriteError(rr, c.err)
		if rr.Code != c.wantCode {
			t.Errorf("code = %d, want %d", rr.Code, c.wantCode)
		}
		var body map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["message"] != c.wantMsg {
			t.Errorf("body.message = %q, want %q", body["message"], c.wantMsg)
		}
		if body["documentation_url"] == "" {
			t.Error("documentation_url should be set")
		}
	}
}

func TestWriteErrorUnknownIs500(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, errOtherForTest())
	if rr.Code != 500 {
		t.Errorf("code = %d, want 500", rr.Code)
	}
}

// errOtherForTest returns a generic non-typed error for the unknown-error path.
func errOtherForTest() error { return &genericError{"boom"} }

type genericError struct{ s string }
func (g *genericError) Error() string { return g.s }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apps/... -run "TestWriteJSONSetsContentType|TestWriteErrorGitHubShape|TestWriteErrorUnknownIs500" -v`
Expected: FAIL — undefined `WriteJSON`, `WriteError`, error vars.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/httpio.go
package apps

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Sentinel errors mapped to GitHub-shape HTTP responses by WriteError.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrUnprocessable = errors.New("unprocessable")
)

// PermissionError is returned by RequirePerm. WriteError maps it to 403.
type PermissionError struct {
	Scope string
	Need  PermissionLevel
}

func (e *PermissionError) Error() string {
	return "permission required: " + e.Scope + ":" + e.Need.String()
}

func WriteJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache")
	w.Header().Set("X-GitHub-Media-Type", "github.v3; format=json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, err error) {
	type body struct {
		Message          string `json:"message"`
		DocumentationURL string `json:"documentation_url"`
	}
	docURL := "https://docs.github.com/rest"

	switch {
	case errors.Is(err, ErrUnauthorized):
		WriteJSON(w, http.StatusUnauthorized, body{Message: "Bad credentials", DocumentationURL: docURL})
	case errors.Is(err, ErrForbidden):
		WriteJSON(w, http.StatusForbidden, body{Message: "Forbidden", DocumentationURL: docURL})
	case errors.Is(err, ErrNotFound):
		WriteJSON(w, http.StatusNotFound, body{Message: "Not Found", DocumentationURL: docURL})
	case errors.Is(err, ErrUnprocessable):
		WriteJSON(w, http.StatusUnprocessableEntity, body{Message: err.Error(), DocumentationURL: docURL})
	default:
		var perm *PermissionError
		if errors.As(err, &perm) {
			WriteJSON(w, http.StatusForbidden, body{
				Message:          "Resource not accessible by integration",
				DocumentationURL: docURL,
			})
			return
		}
		WriteJSON(w, http.StatusInternalServerError, body{Message: "Internal server error", DocumentationURL: docURL})
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/apps/... -run "TestWriteJSONSetsContentType|TestWriteErrorGitHubShape|TestWriteErrorUnknownIs500" -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/httpio.go internal/apps/httpio_test.go
git commit -m "feat(apps): GitHub-shape JSON & error response helpers"
```

---

## Task 9: JWT-auth middleware

**Files:**
- Create: `internal/apps/jwt_middleware.go`
- Create: `internal/apps/jwt_middleware_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/jwt_middleware_test.go
package apps

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestRequireAppJWTMiddleware(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "jwtm-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, secrets, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite},
	})
	defer func() {
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
	}()

	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	priv, _ := x509.ParsePKCS1PrivateKey(block.Bytes)

	verifier := NewJWTVerifier(fs, 60*time.Second)
	handler := RequireAppJWT(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appFromCtx := AppFromContext(r.Context())
		if appFromCtx == nil {
			t.Error("expected App in context")
			http.Error(w, "no app", 500)
			return
		}
		w.WriteHeader(200)
	}))

	t.Run("missing auth", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		handler.ServeHTTP(rr, req)
		if rr.Code != 401 {
			t.Errorf("code = %d, want 401", rr.Code)
		}
	})

	t.Run("valid bearer", func(t *testing.T) {
		now := time.Now()
		tok := signTestJWT(t, app.AppID, priv, now, now.Add(5*time.Minute))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		handler.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("code = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestRequireAppJWTMiddleware -v`
Expected: FAIL — undefined `RequireAppJWT`, `AppFromContext`.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/jwt_middleware.go
package apps

import (
	"context"
	"net/http"
	"strings"
)

type appCtxKey struct{}

func AppFromContext(ctx context.Context) *App {
	a, _ := ctx.Value(appCtxKey{}).(*App)
	return a
}

func WithApp(ctx context.Context, a *App) context.Context {
	return context.WithValue(ctx, appCtxKey{}, a)
}

// RequireAppJWT mounts as chi middleware: verifies a Bearer JWT and injects
// the issuing App into the request context. Used for routes like
// `POST /api/v3/app/installations/{id}/access_tokens` that authenticate with
// an App JWT (not an installation token).
func RequireAppJWT(v *JWTVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractBearer(r)
			if tok == "" {
				WriteError(w, ErrUnauthorized)
				return
			}
			app, err := v.Verify(r.Context(), tok)
			if err != nil || app == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithApp(r.Context(), app)))
		})
	}
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[len("Bearer "):])
}
```

- [ ] **Step 4: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestRequireAppJWTMiddleware -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/jwt_middleware.go internal/apps/jwt_middleware_test.go
git commit -m "feat(apps): chi middleware for App JWT bearer auth"
```

---

## Task 10: Installation-token middleware + permission helper

**Files:**
- Create: `internal/apps/middleware.go`
- Create: `internal/apps/middleware_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/middleware_test.go
package apps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"gitbucket/internal/db"
)

func TestRequireInstallationTokenMiddleware(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "mw-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, _, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite},
	})
	defer func() {
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
	}()
	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         Permissions{"issues": PermWrite},
		Events:              []string{"issues"},
	})
	defer fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(ctx)

	out, _ := MintInstallationToken(ctx, fs, inst, MintRequest{})

	makeHandler := func(scope string, need PermissionLevel) http.Handler {
		return RequireInstallationToken(fs)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := RequirePerm(r.Context(), scope, need); err != nil {
				WriteError(w, err)
				return
			}
			icx := InstallationContextFrom(r.Context())
			if icx == nil || icx.InstallationID != inst.InstallationID {
				t.Error("missing or wrong installation context")
				http.Error(w, "bad ctx", 500)
				return
			}
			w.WriteHeader(200)
		}))
	}

	t.Run("missing token → 401", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		makeHandler("issues", PermRead).ServeHTTP(rr, req)
		if rr.Code != 401 {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("valid bearer + sufficient perm → 200", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		req.Header.Set("Authorization", "Bearer "+out.Plaintext)
		makeHandler("issues", PermRead).ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("code = %d (body: %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("HTTP Basic x-access-token also works", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		req.SetBasicAuth("x-access-token", out.Plaintext)
		makeHandler("issues", PermRead).ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("code = %d (body: %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("insufficient perm → 403", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v3/anything", nil)
		req.Header.Set("Authorization", "Bearer "+out.Plaintext)
		makeHandler("contents", PermWrite).ServeHTTP(rr, req)
		if rr.Code != 403 {
			t.Errorf("code = %d (body: %s)", rr.Code, rr.Body.String())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestRequireInstallationTokenMiddleware -v`
Expected: FAIL — undefined `RequireInstallationToken`, `InstallationContextFrom`, `RequirePerm`.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/middleware.go
package apps

import (
	"context"
	"net/http"
	"strings"

	"cloud.google.com/go/firestore"
)

// InstallationContext is injected into the request context by
// RequireInstallationToken. Handlers read it via InstallationContextFrom.
type InstallationContext struct {
	InstallationID string
	AppID          string
	Account        AccountRef
	Permissions    Permissions
	RepositoryIDs  []string
	BotUserID      string
}

type installationCtxKey struct{}

func InstallationContextFrom(ctx context.Context) *InstallationContext {
	c, _ := ctx.Value(installationCtxKey{}).(*InstallationContext)
	return c
}

func WithInstallationContext(ctx context.Context, c *InstallationContext) context.Context {
	return context.WithValue(ctx, installationCtxKey{}, c)
}

// RequireInstallationToken parses a `ghs_`-prefixed token from Authorization,
// the X-Access-Token header, or HTTP Basic (`x-access-token:<token>`), verifies
// it, and populates InstallationContext on the request.
func RequireInstallationToken(fs *firestore.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractInstallationToken(r)
			if tok == "" {
				WriteError(w, ErrUnauthorized)
				return
			}
			rec, err := VerifyInstallationToken(r.Context(), fs, tok)
			if err != nil || rec == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			inst, err := GetInstallation(r.Context(), fs, rec.InstallationID)
			if err != nil || inst == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			// Read the App row for app_id + bot_user_id. This is one extra
			// point-read per request; a follow-on can cache by app_id with TTL.
			app, err := GetApp(r.Context(), fs, inst.AppID)
			if err != nil || app == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			ic := &InstallationContext{
				InstallationID: inst.InstallationID,
				AppID:          inst.AppID,
				Account:        inst.Account,
				Permissions:    rec.Permissions,
				RepositoryIDs:  rec.RepositoryIDs,
				BotUserID:      app.BotUserID,
			}
			next.ServeHTTP(w, r.WithContext(WithInstallationContext(r.Context(), ic)))
		})
	}
}

// RequirePerm returns nil if the installation context grants `need` on `scope`.
// Returns *PermissionError otherwise, which WriteError maps to 403 with the
// GitHub-shape body "Resource not accessible by integration".
func RequirePerm(ctx context.Context, scope string, need PermissionLevel) error {
	ic := InstallationContextFrom(ctx)
	if ic == nil {
		return ErrUnauthorized
	}
	if !ic.Permissions.Satisfies(scope, need) {
		return &PermissionError{Scope: scope, Need: need}
	}
	return nil
}

func extractInstallationToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		v := strings.TrimSpace(h[len("Bearer "):])
		if strings.HasPrefix(v, tokenPrefix) {
			return v
		}
	}
	if h := r.Header.Get("X-Access-Token"); strings.HasPrefix(h, tokenPrefix) {
		return h
	}
	// HTTP Basic: user = "x-access-token", pass = <token>. Git CLI uses this.
	user, pass, ok := r.BasicAuth()
	if ok && user == "x-access-token" && strings.HasPrefix(pass, tokenPrefix) {
		return pass
	}
	return ""
}
```

- [ ] **Step 4: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestRequireInstallationTokenMiddleware -v
```

Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/middleware.go internal/apps/middleware_test.go
git commit -m "feat(apps): installation-token middleware + RequirePerm helper"
```

---

## Task 11: HTTP handlers for the 4 JWT-authed App endpoints

**Files:**
- Create: `internal/apps/handlers.go`
- Create: `internal/apps/handlers_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/handlers_test.go
package apps

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gitbucket/internal/db"
)

func TestAppHandlersEndToEnd(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "h-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	botUID, _ := CreateBotUser(ctx, fs, slug, slug, "", "pending")
	app, secrets, _ := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite, "contents": PermRead},
		DefaultEvents:      []string{"issues"},
	})
	defer func() {
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx)
	}()
	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         Permissions{"issues": PermWrite, "contents": PermRead},
		Events:              []string{"issues"},
	})
	defer fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(ctx)

	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	priv, _ := x509.ParsePKCS1PrivateKey(block.Bytes)

	h := NewHandler(fs, store, NewJWTVerifier(fs, 60*time.Second))
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	bearer := func() string {
		return "Bearer " + signTestJWT(t, app.AppID, priv, time.Now(), time.Now().Add(5*time.Minute))
	}

	t.Run("GET /api/v3/app returns metadata", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["slug"] != slug {
			t.Errorf("slug = %v", body["slug"])
		}
	})

	t.Run("GET /api/v3/app/installations lists", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app/installations", nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var list []map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &list)
		if len(list) != 1 {
			t.Errorf("len = %d, want 1", len(list))
		}
	})

	t.Run("GET /api/v3/app/installations/{id}", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app/installations/"+inst.InstallationID, nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("POST .../access_tokens mints a token", func(t *testing.T) {
		req := httptest.NewRequest("POST",
			"/api/v3/app/installations/"+inst.InstallationID+"/access_tokens",
			bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", bearer())
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 201 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		tok, _ := body["token"].(string)
		if !strings.HasPrefix(tok, "ghs_") {
			t.Errorf("token prefix wrong: %q", tok)
		}
		if body["expires_at"] == nil {
			t.Error("expires_at missing")
		}
		// Verify it actually works as an installation token.
		if _, err := VerifyInstallationToken(ctx, fs, tok); err != nil {
			t.Errorf("minted token does not verify: %v", err)
		}
	})

	t.Run("POST .../access_tokens with subset perms ok", func(t *testing.T) {
		body := bytes.NewBufferString(`{"permissions":{"issues":"read"}}`)
		req := httptest.NewRequest("POST",
			"/api/v3/app/installations/"+inst.InstallationID+"/access_tokens", body)
		req.Header.Set("Authorization", bearer())
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 201 {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("POST .../access_tokens with super perms → 422", func(t *testing.T) {
		body := bytes.NewBufferString(`{"permissions":{"workflows":"write"}}`)
		req := httptest.NewRequest("POST",
			"/api/v3/app/installations/"+inst.InstallationID+"/access_tokens", body)
		req.Header.Set("Authorization", bearer())
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 422 {
			t.Errorf("code = %d body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("missing JWT → 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/app", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 401 {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("installation owned by other app → 404", func(t *testing.T) {
		// Create another App + Installation; the original JWT should not be
		// able to mint tokens for the second installation.
		slug2 := "h-app2-" + randHex(4)
		botUID2, _ := CreateBotUser(ctx, fs, slug2, slug2, "", "pending")
		app2, _, _ := CreateApp(ctx, fs, store, CreateAppRequest{
			Slug: slug2, Name: slug2, OwnerAccount: owner, BotUserID: botUID2,
			WebhookURL: "https://example.com/y",
		})
		defer func() {
			_, _ = fs.Collection(CollectionApps).Doc(app2.AppID).Delete(ctx)
			_, _ = fs.Collection("usernames").Doc(slug2 + "[bot]").Delete(ctx)
			_, _ = fs.Collection(CollectionUsers).Doc(botUID2).Delete(ctx)
		}()
		inst2, _ := CreateInstallation(ctx, fs, CreateInstallationRequest{
			AppID: app2.AppID, Account: installeeAcct,
			RepositorySelection: "all",
			Permissions:         Permissions{"issues": PermRead},
		})
		defer fs.Collection(CollectionInstallations).Doc(inst2.InstallationID).Delete(ctx)

		req := httptest.NewRequest("GET", "/api/v3/app/installations/"+inst2.InstallationID, nil)
		req.Header.Set("Authorization", bearer())
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404 (body: %s)", rr.Code, rr.Body.String())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestAppHandlersEndToEnd -v`
Expected: FAIL — undefined `NewHandler`, `RegisterRoutes`.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/handlers.go
package apps

import (
	"encoding/json"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	FS    *firestore.Client
	Store SecretStore
	JWT   *JWTVerifier
}

func NewHandler(fs *firestore.Client, store SecretStore, jwt *JWTVerifier) *Handler {
	return &Handler{FS: fs, Store: store, JWT: jwt}
}

// --- Handlers --------------------------------------------------------------

func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	WriteJSON(w, 200, appJSON(app))
}

func (h *Handler) ListInstallations(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	list, err := ListInstallationsForApp(r.Context(), h.FS, app.AppID)
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, i := range list {
		out = append(out, installationJSON(i))
	}
	WriteJSON(w, 200, out)
}

func (h *Handler) GetInstallation(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	id := chi.URLParam(r, "installation_id")
	inst, err := GetInstallationForApp(r.Context(), h.FS, id, app.AppID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if inst == nil {
		WriteError(w, ErrNotFound)
		return
	}
	WriteJSON(w, 200, installationJSON(inst))
}

func (h *Handler) CreateInstallationAccessToken(w http.ResponseWriter, r *http.Request) {
	app := AppFromContext(r.Context())
	id := chi.URLParam(r, "installation_id")
	inst, err := GetInstallationForApp(r.Context(), h.FS, id, app.AppID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if inst == nil {
		WriteError(w, ErrNotFound)
		return
	}

	var req struct {
		Permissions   map[string]string `json:"permissions"`
		RepositoryIDs []string          `json:"repository_ids"`
	}
	// Body is optional. Ignore decode errors for empty bodies.
	_ = json.NewDecoder(r.Body).Decode(&req)

	mintReq := MintRequest{}
	if len(req.Permissions) > 0 {
		mintReq.Permissions = Permissions{}
		for k, v := range req.Permissions {
			mintReq.Permissions[k] = ParsePermissionLevel(v)
		}
	}
	if len(req.RepositoryIDs) > 0 {
		mintReq.RepositoryIDs = req.RepositoryIDs
	}

	out, err := MintInstallationToken(r.Context(), h.FS, inst, mintReq)
	if err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}

	WriteJSON(w, 201, map[string]interface{}{
		"token":                out.Plaintext,
		"expires_at":           out.Record.ExpiresAt.UTC().Format(time.RFC3339),
		"permissions":          permissionsJSON(out.Record.Permissions),
		"repository_selection": inst.RepositorySelection,
		"single_file":          nil,
		// repositories array is left empty in Plan 1; populated in Plan 2 when
		// the repository formatter exists. Apps using repository_selection ==
		// "all" don't strictly need it, and "selected" installations get an
		// empty list (acceptable for MVP).
		"repositories": []interface{}{},
	})
}

// --- JSON shape helpers (lightweight; full formatters live in Plan 2) -----

func appJSON(a *App) map[string]interface{} {
	return map[string]interface{}{
		"id":                  a.AppID,
		"slug":                a.Slug,
		"name":                a.Name,
		"owner":               map[string]interface{}{"login": a.OwnerAccount.ID, "type": string(a.OwnerAccount.Type)},
		"client_id":           a.ClientID,
		"permissions":         permissionsJSON(a.DefaultPermissions),
		"events":              a.DefaultEvents,
		"created_at":          a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func installationJSON(i *Installation) map[string]interface{} {
	return map[string]interface{}{
		"id":                   i.InstallationID,
		"app_id":               i.AppID,
		"account":              map[string]interface{}{"login": i.Account.ID, "type": string(i.Account.Type)},
		"repository_selection": i.RepositorySelection,
		"permissions":          permissionsJSON(i.Permissions),
		"events":               i.Events,
		"created_at":           i.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func permissionsJSON(p Permissions) map[string]string {
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = v.String()
	}
	return out
}
```

- [ ] **Step 4: Create the routes file**

```go
// internal/apps/routes.go
package apps

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the /api/v3/app/* routes (JWT-authed). The
// installation-token-authed endpoints (e.g. repos, issues, pulls) will be
// mounted by Plan 2 via a separate RegisterV3Routes function.
func RegisterRoutes(r chi.Router, h *Handler) {
	r.Route("/api/v3/app", func(r chi.Router) {
		r.Use(RequireAppJWT(h.JWT))
		r.Get("/", h.GetApp)
		r.Get("/installations", h.ListInstallations)
		r.Get("/installations/{installation_id}", h.GetInstallation)
		r.Post("/installations/{installation_id}/access_tokens", h.CreateInstallationAccessToken)
	})
}
```

- [ ] **Step 5: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestAppHandlersEndToEnd -v
```

Expected: all subtests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/handlers.go internal/apps/routes.go internal/apps/handlers_test.go
git commit -m "feat(apps): HTTP handlers for /api/v3/app/* (4 JWT-authed endpoints)"
```

---

## Task 12: Wire routes into main.go

**Files:**
- Modify: `internal/config/config.go` — add `SecretManagerProject` field.
- Modify: `main.go` — initialize SecretStore + JWTVerifier + apps.Handler and mount routes.

- [ ] **Step 1: Update Config**

Open `internal/config/config.go` and add the field + default to `Config` and `Load`:

```go
// Config holds the application configuration.
type Config struct {
	Port                 string
	GCSBucket            string
	DevMode              bool
	RestrictedIP         string
	ProjectID            string
	LocalReposRoot       string
	KMSKeyName           string
	SecretManagerProject string // defaults to ProjectID
}
```

Inside `Load()`, after the existing `kmsKeyName` block, add:

```go
secretManagerProject := os.Getenv("SECRET_MANAGER_PROJECT")
if secretManagerProject == "" {
	secretManagerProject = projectID
}
```

And include it in the returned struct:

```go
return &Config{
	// ...existing fields...
	SecretManagerProject: secretManagerProject,
}
```

- [ ] **Step 2: Initialize SecretStore in `main.go`**

In `main.go`, after the Firestore client is created and before the chi router is constructed, add:

```go
import (
    // existing imports...
    secretmanager "cloud.google.com/go/secretmanager/apiv1"

    "gitbucket/internal/apps"
)

// ... inside main(), after firestoreClient is set up:
var appsSecretStore apps.SecretStore
if cfg.DevMode {
    appsSecretStore = apps.NewMemorySecretStore()
    log.Println("apps: using in-memory SecretStore (DEV_MODE)")
} else {
    smClient, err := secretmanager.NewClient(ctx)
    if err != nil {
        log.Fatalf("failed to initialize Secret Manager client: %v", err)
    }
    defer smClient.Close()
    appsSecretStore = apps.NewRealSecretStore(smClient, cfg.SecretManagerProject)
    log.Printf("apps: Secret Manager client initialized for project %s", cfg.SecretManagerProject)
}
```

- [ ] **Step 3: Construct Handler and mount routes**

After the existing `apiHandler.RegisterRoutes(r)` line, add:

```go
// GitHub App emulation routes (Plan 1: JWT-authed App endpoints only).
appsJWTVerifier := apps.NewJWTVerifier(firestoreClient, 60*time.Second)
appsHandler := apps.NewHandler(firestoreClient, appsSecretStore, appsJWTVerifier)
apps.RegisterRoutes(r, appsHandler)
```

You may need to add `"time"` to the imports if not already present.

Also update the SPA fallback guard in `main.go` to exclude `/api/v3/` from the SPA route. Find the line:

```go
if strings.HasPrefix(req.URL.Path, "/api/") || strings.HasPrefix(req.URL.Path, "/r/") {
```

The `/api/` prefix already covers `/api/v3/`, so no change is required. Confirm by reading the surrounding code; if the prefix list ever becomes more specific, add `/api/v3/` explicitly.

- [ ] **Step 4: Build the binary to confirm it compiles**

```bash
go build -o /tmp/gitbucket-plan1 main.go
```

Expected: build succeeds, no errors. Delete the binary if you don't need it (`rm /tmp/gitbucket-plan1`).

- [ ] **Step 5: Run the full Go test suite to confirm no regressions**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/...
```

Expected: all PASS, including the existing `internal/api` tests.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go main.go
git commit -m "feat(apps): wire /api/v3/app routes into main + Secret Manager init"
```

---

## Task 13: Test fixtures package for use by Plan 2+

**Files:**
- Create: `internal/apps/testfixtures/fixtures.go`
- Create: `internal/apps/testfixtures/fixtures_test.go`

This package is consumed by integration tests in Plan 2 (REST surface) and Plan 3 (webhook engine). It lives under a subpackage so production builds don't depend on it.

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/testfixtures/fixtures_test.go
package testfixtures

import (
	"context"
	"os"
	"strings"
	"testing"

	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

func TestNewTestAppAndInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	if scen.App == nil || scen.App.AppID == "" {
		t.Fatal("scenario should have an App")
	}
	if scen.Installation == nil {
		t.Fatal("scenario should have an Installation")
	}
	if scen.PrivateKey == nil {
		t.Fatal("scenario should expose the private key for signing JWTs")
	}

	jwt := scen.SignJWT(t)
	if !strings.HasPrefix(jwt, "ey") { // base64 header prefix for {"alg":...}
		t.Errorf("jwt does not look like a JWS: %q", jwt[:5])
	}

	tok, err := scen.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if !strings.HasPrefix(tok, "ghs_") {
		t.Errorf("token prefix wrong: %q", tok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/testfixtures/... -v`
Expected: FAIL — undefined `NewScenario`.

- [ ] **Step 3: Write the implementation**

```go
// internal/apps/testfixtures/fixtures.go
// Package testfixtures provides reusable seed helpers for tests that need an
// App + Installation + working installation token. Used by tests in plans
// 2, 3, and 4. NOT for production use.
package testfixtures

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/golang-jwt/jwt/v4"

	"gitbucket/internal/apps"
)

// Scenario bundles a freshly-seeded App + Installation + decoded private key,
// so a test can sign JWTs and mint installation tokens with one line.
type Scenario struct {
	FS           *firestore.Client
	Store        apps.SecretStore
	App          *apps.App
	Installation *apps.Installation
	PrivateKey   *rsa.PrivateKey
	BotUID       string
}

// NewScenario seeds Firestore with an App + bot user + Installation and
// returns a Scenario ready for use. The caller MUST defer scen.Cleanup(ctx).
func NewScenario(t *testing.T, ctx context.Context, fs *firestore.Client) *Scenario {
	t.Helper()
	store := apps.NewMemorySecretStore()

	suffix := randHex(4)
	slug := "fx-" + suffix
	owner := apps.AccountRef{ID: "owner-" + suffix, Type: apps.AccountTypeUser}

	botUID, err := apps.CreateBotUser(ctx, fs, slug, slug, "", "pending")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}

	app, secrets, err := apps.CreateApp(ctx, fs, store, apps.CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL: "https://example.test/hook",
		DefaultPermissions: apps.Permissions{
			"issues": apps.PermWrite, "contents": apps.PermWrite,
			"pull_requests": apps.PermWrite, "metadata": apps.PermRead,
		},
		DefaultEvents: []string{"issues", "issue_comment", "pull_request"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	installeeAcct := apps.AccountRef{ID: "acct-" + suffix, Type: apps.AccountTypeUser}
	inst, err := apps.CreateInstallation(ctx, fs, apps.CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         app.DefaultPermissions,
		Events:              app.DefaultEvents,
	})
	if err != nil {
		t.Fatalf("CreateInstallation: %v", err)
	}

	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse pkcs1: %v", err)
	}

	return &Scenario{
		FS: fs, Store: store, App: app, Installation: inst, PrivateKey: priv, BotUID: botUID,
	}
}

// SignJWT returns a valid RS256 JWT for s.App, expiring in 5 minutes.
func (s *Scenario) SignJWT(t *testing.T) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": s.App.AppID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	str, err := tok.SignedString(s.PrivateKey)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return str
}

// MintToken issues a fresh installation token for s.Installation.
func (s *Scenario) MintToken(ctx context.Context) (string, error) {
	out, err := apps.MintInstallationToken(ctx, s.FS, s.Installation, apps.MintRequest{})
	if err != nil {
		return "", err
	}
	return out.Plaintext, nil
}

// Cleanup deletes all seeded Firestore docs. Idempotent.
func (s *Scenario) Cleanup(ctx context.Context) {
	_, _ = s.FS.Collection(apps.CollectionInstallations).Doc(s.Installation.InstallationID).Delete(ctx)
	_, _ = s.FS.Collection(apps.CollectionApps).Doc(s.App.AppID).Delete(ctx)
	_, _ = s.FS.Collection("usernames").Doc(s.App.Slug + "[bot]").Delete(ctx)
	_, _ = s.FS.Collection(apps.CollectionUsers).Doc(s.BotUID).Delete(ctx)
	// Tokens issued during the test live under installation_tokens/{hash}; we
	// leave them to Firestore TTL since their doc IDs are not tracked here.
	_ = ctx
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/testfixtures/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/testfixtures/fixtures.go internal/apps/testfixtures/fixtures_test.go
git commit -m "test(apps): shared test fixtures (Scenario, SignJWT, MintToken)"
```

---

## Task 14: End-to-end auth-plane integration test

**Files:**
- Create: `internal/apps/e2e_test.go`

This is the final-acceptance test for Plan 1: a single test that drives the full chi router from outside, mints a JWT, exchanges it for an installation token, verifies the response shape, and confirms the token actually authenticates the (yet-unimplemented but library-ready) installation-token middleware.

- [ ] **Step 1: Write the test**

```go
// internal/apps/e2e_test.go
package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestPlan1AuthPlaneEndToEnd(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	r := chi.NewRouter()
	jwtV := NewJWTVerifier(fs, 60*time.Second)
	h := NewHandler(fs, scen.Store, jwtV)
	RegisterRoutes(r, h)

	// Also mount a probe endpoint behind RequireInstallationToken to prove the
	// minted token works end-to-end as bearer credential.
	r.With(RequireInstallationToken(fs)).Get("/__probe", func(w http.ResponseWriter, req *http.Request) {
		ic := InstallationContextFrom(req.Context())
		if ic == nil || ic.AppID == "" {
			http.Error(w, "no installation context", 500)
			return
		}
		WriteJSON(w, 200, map[string]string{"installation_id": ic.InstallationID})
	})

	jwtStr := scen.SignJWT(t)

	// Step 1: Mint a token via the public HTTP endpoint.
	req := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("mint: code = %d body: %s", rr.Code, rr.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &minted)
	tok, _ := minted["token"].(string)
	if !strings.HasPrefix(tok, "ghs_") {
		t.Fatalf("token prefix wrong: %q", tok)
	}
	if minted["expires_at"] == nil {
		t.Fatal("expires_at missing")
	}
	if minted["permissions"] == nil {
		t.Fatal("permissions missing")
	}

	// Step 2: Use the token on a protected route.
	req2 := httptest.NewRequest("GET", "/__probe", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("probe: code = %d body: %s", rr2.Code, rr2.Body.String())
	}
	var probeBody map[string]string
	_ = json.Unmarshal(rr2.Body.Bytes(), &probeBody)
	if probeBody["installation_id"] != scen.Installation.InstallationID {
		t.Errorf("probe returned installation %q, want %q", probeBody["installation_id"], scen.Installation.InstallationID)
	}
}
```

- [ ] **Step 2: Run the test**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestPlan1AuthPlaneEndToEnd -v
```

Expected: PASS.

- [ ] **Step 3: Run the full repo test suite one last time**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/...
```

Expected: all PASS — no regressions in existing `internal/api`, `internal/auth`, `internal/db` packages.

- [ ] **Step 4: Document the one-time Firestore TTL configuration**

Append a short README inside `internal/apps/`:

```bash
cat > internal/apps/README.md <<'EOF'
# Apps Package — Operational Notes

## One-time Firestore TTL setup (per environment)

Installation tokens carry an `expires_at` field; Firestore native TTL deletes
expired docs on a periodic sweep. Configure once per project:

```
gcloud firestore fields ttls update expires_at \
    --collection-group=installation_tokens \
    --enable-ttl
```

The middleware (`RequireInstallationToken`) ALSO checks `expires_at` against
now on every read — the TTL is cleanup, not enforcement. So a missed TTL sweep
does not create a security issue, only storage cruft.
EOF
```

Then:

```bash
git add internal/apps/README.md internal/apps/e2e_test.go
git commit -m "test(apps): plan-1 end-to-end auth plane test + ops README"
```

---

## Self-Review

### Spec coverage

Walking through the spec sections that are in Plan 1 scope:

- **§3 Ownership Model** (polymorphic `account` reference) — covered by `AccountRef` + `AccountType` (Task 1).
- **§4 Data Model**:
  - `apps/{app_id}` → Task 1 (type) + Task 4 (CRUD).
  - `installations/{installation_id}` → Task 1 (type) + Task 5 (CRUD).
  - `installation_tokens/{token_sha256}` → Task 1 (type) + Task 7 (mint/verify).
  - `webhook_deliveries/{delivery_id}` → out of Plan 1 scope (Plan 3).
  - No-plaintext-secrets / O(1) token lookup → Tasks 4 (sha256 hashing) + 7 (doc-id-is-hash).
  - Firestore TTL → documented in Task 14 README; field present in Task 1.
- **§5 Auth Plane**:
  - §5.1 JWT verification → Task 6.
  - §5.2 Installation token issuance → Tasks 7 (library) + 11 (HTTP handler).
  - §5.3 Request auth middleware → Task 10 (token middleware) + Task 9 (JWT middleware).
  - Permission helper → Task 10.
- **§9.1 Username regex change** — Task 2.

Out of Plan 1 scope (deferred):
- §6 GitHub-shape REST surface beyond the 4 App endpoints → Plan 2.
- §7 Outbound webhook engine → Plan 3.
- §8 Manifest registration flow → Plan 4.
- §9.2 Loop-prevention wire-up → Plan 3 (lands with the events engine).
- §9.4 Bot users cannot be impersonated — partial: the user-resolver-layer block on bot writes is NOT in Plan 1. Add a note to Plan 4 to reject bot UIDs in the existing user resolver.
- §10 Testing strategy: Layer 1 and Layer 2 covered for the auth plane; Layer 3 (fake-app e2e) lands in Plan 4 when there's a full flow to exercise.

### Placeholder scan

The `firestoreUpdate`/`firestoreUpdateRaw` scaffolding in the Task 6 and Task 7 test bodies is annotated as a transcription convenience and labelled with explicit replacement instructions. Engineers MUST use the noted real `firestore.Update` import when writing the test. This is the only place in the plan with deliberate placeholder shape, and it's flagged loudly. All other steps contain complete, executable content.

### Type consistency

- `Permissions` map type, `PermissionLevel` enum, `AccountRef`, `AccountType` — defined once in Task 1, referenced consistently in Tasks 4–11.
- `App`, `Installation`, `InstallationToken` — defined once in Task 1, used by Tasks 4–11.
- `MintRequest`/`MintOutput` — defined in Task 7, consumed in Task 11.
- `CreateAppRequest`/`AppSecrets` — defined in Task 4, used in Tasks 5, 6, 7, 9, 10, 11.
- `CreateInstallationRequest` — defined in Task 5, used in Tasks 6, 7, 10, 11.
- `JWTVerifier`/`NewJWTVerifier` — defined in Task 6, wired by Tasks 9, 11.
- `SecretStore`/`MemorySecretStore`/`RealSecretStore` — defined in Task 3, consumed by Tasks 4, 11, 12.
- `WriteJSON`/`WriteError`/`ErrUnauthorized`/`ErrForbidden`/`ErrNotFound`/`ErrUnprocessable`/`PermissionError` — defined in Task 8, used by Tasks 9, 10, 11.
- `InstallationContext`/`InstallationContextFrom`/`RequirePerm` — defined in Task 10, used by Task 11 e2e probe + Task 14.
- `Handler`/`NewHandler`/`RegisterRoutes` — defined in Task 11, mounted in Task 12.
- Test fixture `Scenario` — defined in Task 13, used in Task 14.

No drift between definitions and usages.

### Scope check

Plan 1 produces standalone, demoable software: a GitBucket binary that accepts App JWTs, mints `ghs_...` tokens, and has a tested middleware library ready for Plan 2 to use on real endpoints. Each task is independent enough that an executing subagent can complete it in 5–20 minutes, commit, and move to the next.
