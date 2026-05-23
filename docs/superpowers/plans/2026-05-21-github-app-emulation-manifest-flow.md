# GitHub App Emulation — Plan 4: Manifest Registration Flow + SPA UI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the GitHub App manifest registration flow + the SPA pages that drive it, so a third-party agent like Claude Code can point at a GitBucket instance, walk the standard 3-hop manifest dance, install onto repos, and start operating end-to-end. Also lands the bot-impersonation guards from spec §9.4 (reject `type:"Bot"` users from login + PAT creation paths).

**Architecture:** Three new backend endpoints (`POST /api/v3/settings/apps/manifest-conversions`, `POST /api/v3/app-manifests/{code}/conversions`, `POST /api/v3/user/installations`) + a `ManifestConversion` Firestore collection with 10-minute TTL + three new SPA pages (`/settings/apps/new`, `/settings/apps/{slug}/installations/new`, `/settings/apps`). The installation endpoint fires an `installation:created` event via `apps.Fire` (Plan 3's events package). Bot-user write guards are added at the user-resolver layer in `internal/db/db.go` so a stolen Firebase token can't act as a bot. The manifest hop-1 confirmation page is pure SPA — no new backend route, served by the existing catch-all.

**Tech Stack:** Same as Plans 1-3 — Go, chi v5, Firestore, React (SPA). No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-05-20-github-app-emulation-design.md` §8, §9.4
**Builds on:** `docs/superpowers/plans/2026-05-21-github-app-emulation-webhook-engine.md` (Plan 3)

**Scope of Plan 4:** Spec §8 (manifest registration + installation flows + UX surface) + §9.4 (bot impersonation guards).

**Out of scope of Plan 4 (deferred):**
- Issues + `issue_comment` events — Plan 2.5 (no issues backend exists yet)
- Full admin UI (delivery logs, suspend/unsuspend, secret rotation, redeliver) — follow-on after MVP is shipped
- User-to-server OAuth (`/login/oauth/authorize`, `/login/oauth/access_token`) — needed for Apps that act *as a user*, not as the bot. Separate spec.

**Branch base:** Stack on `feature/github-app-emulation-webhook-engine` (Plan 3, currently in PR). Suggested branch name: `feature/github-app-emulation-manifest-flow`. If Plans 1-3 merge to main first, rebase onto main.

---

## File Structure

**New files:**

```
internal/apps/
  manifest.go                — ManifestConversion type + Firestore CRUD + 10-min TTL
  manifest_test.go
  manifest_handlers.go       — hop 2 (POST /api/v3/settings/apps/manifest-conversions)
                              + hop 3 (POST /api/v3/app-manifests/{code}/conversions)
  manifest_handlers_test.go
  installations_handler.go   — POST /api/v3/user/installations (web-authed install flow)
  installations_handler_test.go
  e2e_manifest_test.go       — full 3-hop conversion test with fake Claude callback receiver

frontend/src/pages/
  SettingsApps.jsx           — list of owned + installed Apps
  SettingsAppsNew.jsx        — manifest confirmation card (hop 1)
  SettingsAppsInstall.jsx    — install repo picker

frontend/src/utils/
  manifestUrl.js             — helper for parsing the `?manifest=` query param
```

**Modified files:**

```
internal/apps/routes.go      — register 3 new endpoints
internal/apps/handlers.go    — extend Handler with WebAuth (needed for the 2 web-authed endpoints)
internal/db/db.go            — bot impersonation guards in PAT/login paths (see §9.4)
internal/auth/auth.go        — bot impersonation guard in WebAuth middleware (defense in depth)
main.go                      — pass authHandler into apps.Handler
frontend/src/App.jsx         — add 3 SPA routes
frontend/src/apiClient.js    — add manifest + installation methods
```

**Manual ops (none new):** Plan 1's Firestore TTL on installation_tokens still applies. Configure one more TTL for manifest_conversions (10 minutes via the doc's expires_at field):

```bash
gcloud firestore fields ttls update expires_at \
    --collection-group=manifest_conversions \
    --enable-ttl
```

---

## Task 1: ManifestConversion data layer

**Files:**
- Create: `internal/apps/manifest.go`
- Create: `internal/apps/manifest_test.go`

The `manifest_conversions/{code}` Firestore doc records a one-time code that Claude exchanges for the App's plaintext secrets. 10-minute TTL via the `expires_at` field; document is deleted after a successful exchange (and the TTL sweep cleans up un-exchanged codes).

- [ ] **Step 1: Write the failing test**

Create `internal/apps/manifest_test.go`:

```go
package apps

import (
	"context"
	"os"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestCreateAndConsumeManifestConversion(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	code, err := CreateManifestConversion(ctx, fs, "app-id-xyz")
	if err != nil {
		t.Fatalf("CreateManifestConversion: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionManifestConversions).Doc(code).Delete(context.Background())
	})
	if code == "" {
		t.Fatal("code should be non-empty")
	}

	appID, err := ConsumeManifestConversion(ctx, fs, code)
	if err != nil {
		t.Fatalf("ConsumeManifestConversion: %v", err)
	}
	if appID != "app-id-xyz" {
		t.Errorf("appID = %q, want app-id-xyz", appID)
	}

	// Second consume must fail — single-use.
	if _, err := ConsumeManifestConversion(ctx, fs, code); err == nil {
		t.Error("expected error on second Consume (single-use)")
	}
}

func TestConsumeManifestConversion_NotFound(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	_, err := ConsumeManifestConversion(ctx, fs, "no-such-code")
	if err == nil {
		t.Error("expected error for unknown code")
	}
}

func TestConsumeManifestConversion_Expired(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	code, _ := CreateManifestConversion(ctx, fs, "app-id-exp")
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionManifestConversions).Doc(code).Delete(context.Background())
	})
	// Backdate expires_at.
	if err := backdateConversionForTest(ctx, fs, code, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := ConsumeManifestConversion(ctx, fs, code); err == nil {
		t.Error("expected error for expired code")
	}
}

// backdateConversionForTest is a test helper that mutates the expires_at
// field directly (real callers don't need this).
func backdateConversionForTest(ctx context.Context, fs interface{}, code string, when time.Time) error {
	// Implementer: use cloud.google.com/go/firestore here.
	// Pseudocode:
	//   _, err := fs.(*firestore.Client).Collection(CollectionManifestConversions).Doc(code).
	//       Update(ctx, []firestore.Update{{Path: "expires_at", Value: when.UTC()}})
	//   return err
	_, _, _ = ctx, code, when
	return nil
}
```

When you write this file, REPLACE the `backdateConversionForTest` stub body with the real `firestore.Update` call (the function signature should take `*firestore.Client` not `interface{}` — that was just a way to keep the test's import list minimal in the plan text). Add `"cloud.google.com/go/firestore"` to imports.

- [ ] **Step 2: Confirm fail**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateAndConsumeManifestConversion -v
```

Expected: undefined `CreateManifestConversion`, `ConsumeManifestConversion`, `CollectionManifestConversions`.

- [ ] **Step 3: Implementation**

Create `internal/apps/manifest.go`:

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

// CollectionManifestConversions is the Firestore collection name for
// one-time codes that Claude/Jules exchange for App plaintext secrets after
// completing the manifest registration flow.
const CollectionManifestConversions = "manifest_conversions"

// ManifestConversionTTL is how long a code remains valid after creation.
// Matches spec §8.1.
const ManifestConversionTTL = 10 * time.Minute

// ManifestConversion is the persisted record. The doc ID is the code itself
// (single-use, opaque to the client). expires_at is enforced by both the
// Firestore TTL policy and an explicit check in Consume.
type ManifestConversion struct {
	Code      string    `firestore:"code"`
	AppID     string    `firestore:"app_id"`
	CreatedAt time.Time `firestore:"created_at"`
	ExpiresAt time.Time `firestore:"expires_at"`
}

// CreateManifestConversion writes a fresh code → app_id mapping with a
// 10-minute expiry. The code is returned to the SPA, which redirects the
// browser back to Claude/Jules with `?code=<code>`.
func CreateManifestConversion(ctx context.Context, fs *firestore.Client, appID string) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	if appID == "" {
		return "", fmt.Errorf("appID is required")
	}
	codeBytes := make([]byte, 24)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	code := hex.EncodeToString(codeBytes)
	now := time.Now().UTC()
	rec := ManifestConversion{
		Code:      code,
		AppID:     appID,
		CreatedAt: now,
		ExpiresAt: now.Add(ManifestConversionTTL),
	}
	if _, err := fs.Collection(CollectionManifestConversions).Doc(code).Create(ctx, rec); err != nil {
		return "", fmt.Errorf("write manifest conversion: %w", err)
	}
	return code, nil
}

// ConsumeManifestConversion atomically reads the doc, deletes it, and
// returns the associated app_id. Single-use: a second call with the same
// code returns an error. Also rejects expired codes (TTL is cleanup, not
// real-time enforcement).
func ConsumeManifestConversion(ctx context.Context, fs *firestore.Client, code string) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	docRef := fs.Collection(CollectionManifestConversions).Doc(code)

	var appID string
	err := fs.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			if isFirestoreNotFound(err) {
				return fmt.Errorf("conversion not found")
			}
			return err
		}
		var mc ManifestConversion
		if err := doc.DataTo(&mc); err != nil {
			return err
		}
		if time.Now().After(mc.ExpiresAt) {
			return fmt.Errorf("conversion expired")
		}
		appID = mc.AppID
		return tx.Delete(docRef)
	})
	return appID, err
}
```

- [ ] **Step 4: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run "TestCreateAndConsumeManifestConversion|TestConsumeManifestConversion" -v
go vet ./internal/apps/...

git add internal/apps/manifest.go internal/apps/manifest_test.go
git commit -m "feat(apps): ManifestConversion data layer (10-min TTL, single-use codes)"
```

Report Status, commit SHA, test output.

---

## Task 2: Manifest hop 2 endpoint — create the App from the manifest

**Files:**
- Create: `internal/apps/manifest_handlers.go` (new file; will hold both hop 2 and hop 3 handlers)
- Create: `internal/apps/manifest_handlers_test.go`
- Modify: `internal/apps/handlers.go` — extend `Handler` to carry `*auth.AuthHandler`
- Modify: `internal/apps/routes.go` — register the two new endpoints
- Modify: `main.go` — pass `authHandler` into `apps.NewHandler`

POST `/api/v3/settings/apps/manifest-conversions` requires Firebase web auth — the logged-in user becomes the App's owner. The handler does all the heavy lifting from spec §8.1 hop 2:

1. Validate manifest (required fields, permission values, event names)
2. Allocate slug from manifest.name (kebab-case + collision suffix)
3. Generate App credentials (keypair, secrets, IDs) via existing `apps.CreateApp`
4. Auto-create the synthetic bot user (already done by Plan 1's `CreateBotUser`)
5. Store a `ManifestConversion{code → app_id}` (Task 1's `CreateManifestConversion`)
6. Return `{ redirect_url: "<manifest.redirect_url>?code=<code>" }`

- [ ] **Step 1: Extend Handler with WebAuth**

In `internal/apps/handlers.go`, modify the `Handler` struct:

```go
import (
	// existing imports
	"gitbucket/internal/auth"
)

type Handler struct {
	FS    *firestore.Client
	Store SecretStore
	JWT   *JWTVerifier
	Auth  *auth.AuthHandler // for web-authed manifest endpoints
}

func NewHandler(fs *firestore.Client, store SecretStore, jwt *JWTVerifier, authH *auth.AuthHandler) *Handler {
	return &Handler{FS: fs, Store: store, JWT: jwt, Auth: authH}
}
```

This is a breaking change to `NewHandler`'s signature. Update the call site in `main.go` to pass `authHandler`. Also update test call sites (`handlers_test.go`, `e2e_test.go`, `e2e_webhook_test.go`, etc.) to pass `nil` for the `authH` parameter where they don't need it — the manifest endpoints aren't exercised by those tests.

Find all callers:

```bash
grep -rn "apps.NewHandler\|NewHandler(fs, scen.Store" internal/ main.go
```

Update each to add `nil` (or the real auth handler in main.go) as the 4th argument.

- [ ] **Step 2: Write the failing handler test**

Create `internal/apps/manifest_handlers_test.go`:

```go
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

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestManifestConversionEndpoint(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	authH := auth.NewAuthHandler(true, nil, fs) // DevMode = true; mock_<uid> Bearer tokens work
	store := NewMemorySecretStore()
	jwt := NewJWTVerifier(fs, 60*time.Second)
	h := NewHandler(fs, store, jwt, authH)

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	suffix := randHex(4)
	uid := "manifest-uid-" + suffix
	username := "manifest-user-" + suffix
	// Pre-register the username so the manifest handler can look up the owner.
	if err := db.RegisterUsername(ctx, fs, uid, username, uid+"@test"); err != nil {
		t.Fatalf("RegisterUsername: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(username).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	manifest := map[string]interface{}{
		"name":             "Test Manifest App " + suffix,
		"url":              "https://example.test",
		"hook_attributes":  map[string]interface{}{"url": "https://example.test/webhook"},
		"redirect_url":     "https://example.test/setup/callback",
		"public":           false,
		"default_permissions": map[string]interface{}{
			"contents":     "write",
			"issues":       "write",
			"pull_requests": "write",
			"metadata":     "read",
		},
		"default_events": []string{"issues", "issue_comment", "pull_request"},
	}
	body, _ := json.Marshal(map[string]interface{}{"manifest": manifest})
	req := httptest.NewRequest("POST", "/api/v3/settings/apps/manifest-conversions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	redirectURL, _ := resp["redirect_url"].(string)
	if !strings.HasPrefix(redirectURL, "https://example.test/setup/callback?code=") {
		t.Errorf("redirect_url = %q (expected to start with the manifest's redirect_url + ?code=)", redirectURL)
	}

	// Extract the code and exchange it via hop 3.
	code := strings.TrimPrefix(redirectURL, "https://example.test/setup/callback?code=")
	if code == "" {
		t.Fatal("could not extract code")
	}

	exchangeReq := httptest.NewRequest("POST", "/api/v3/app-manifests/"+code+"/conversions", nil)
	exchangeRR := httptest.NewRecorder()
	r.ServeHTTP(exchangeRR, exchangeReq)
	if exchangeRR.Code != http.StatusOK {
		t.Fatalf("exchange code = %d body: %s", exchangeRR.Code, exchangeRR.Body.String())
	}
	var bundle map[string]interface{}
	_ = json.Unmarshal(exchangeRR.Body.Bytes(), &bundle)
	for _, key := range []string{"id", "slug", "name", "owner", "client_id", "client_secret", "webhook_secret", "pem"} {
		if _, ok := bundle[key]; !ok {
			t.Errorf("bundle missing key %q", key)
		}
	}
	if pem, _ := bundle["pem"].(string); !strings.Contains(pem, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("pem not a PKCS1 RSA private key, got: %q", pem)
	}

	// Cleanup the App + bot user.
	appID, _ := bundle["id"].(string)
	slug, _ := bundle["slug"].(string)
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionApps).Doc(appID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		docs, _ := fs.Collection(CollectionUsers).Where("username", "==", slug+"[bot]").Documents(cctx).GetAll()
		for _, d := range docs {
			_, _ = d.Ref.Delete(cctx)
		}
	})

	// Second exchange must fail (single-use).
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, httptest.NewRequest("POST", "/api/v3/app-manifests/"+code+"/conversions", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("second exchange code = %d, want 404", rr2.Code)
	}
}

func TestManifestConversionRejectsUnauthenticated(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, NewMemorySecretStore(), NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	req := httptest.NewRequest("POST", "/api/v3/settings/apps/manifest-conversions",
		strings.NewReader(`{"manifest":{}}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}
```

- [ ] **Step 3: Confirm fail**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run "TestManifestConversionEndpoint|TestManifestConversionRejectsUnauthenticated" -v
```

Expected: undefined handlers + routes.

- [ ] **Step 4: Write the handlers**

Create `internal/apps/manifest_handlers.go`:

```go
package apps

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/db"
)

// ManifestRequest is the inbound JSON for the hop-2 endpoint. Matches
// GitHub's manifest schema (subset GitBucket implements).
type ManifestRequest struct {
	Manifest ManifestDoc `json:"manifest"`
}

type ManifestDoc struct {
	Name               string                 `json:"name"`
	URL                string                 `json:"url"`
	HookAttributes     ManifestHookAttributes `json:"hook_attributes"`
	RedirectURL        string                 `json:"redirect_url"`
	CallbackURLs       []string               `json:"callback_urls"`
	Public             bool                   `json:"public"`
	DefaultPermissions map[string]string      `json:"default_permissions"`
	DefaultEvents      []string               `json:"default_events"`
}

type ManifestHookAttributes struct {
	URL string `json:"url"`
}

// validManifestEvents is the allow-list of event names a manifest may
// request. Anything outside this set is silently dropped (matches GitHub's
// permissive behavior).
var validManifestEvents = map[string]bool{
	"issues":                      true,
	"issue_comment":               true,
	"pull_request":                true,
	"pull_request_review_comment": true,
	"push":                        true,
	"installation":                true,
}

// slugAllowed restricts the characters that may appear in a generated slug.
// Mirrors the user-facing username regex from internal/api/api.go.
var slugAllowed = regexp.MustCompile(`[^a-z0-9-]+`)

// CreateManifestApp handles POST /api/v3/settings/apps/manifest-conversions.
// Requires Firebase web auth; the logged-in user becomes the App owner.
func (h *Handler) CreateManifestApp(w http.ResponseWriter, r *http.Request) {
	// 1. Authenticate via web auth (Firebase ID token or mock_<uid> in DevMode).
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}
	username, err := db.GetUsernameByUID(r.Context(), h.FS, uid)
	if err != nil || username == "" {
		WriteError(w, ErrUnauthorized)
		return
	}

	// 2. Parse + validate the manifest.
	var req ManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}
	m := req.Manifest
	if m.Name == "" || m.URL == "" || m.HookAttributes.URL == "" || m.RedirectURL == "" {
		WriteError(w, ErrUnprocessable)
		return
	}

	// 3. Validate + filter permissions (drop unknown scopes; keep known ones).
	perms := Permissions{}
	for scope, level := range m.DefaultPermissions {
		lv := ParsePermissionLevel(level)
		if lv == PermNone {
			continue
		}
		perms[scope] = lv
	}
	// Filter events to the allow-list.
	events := make([]string, 0, len(m.DefaultEvents))
	for _, e := range m.DefaultEvents {
		if validManifestEvents[e] {
			events = append(events, e)
		}
	}

	// 4. Allocate a unique slug.
	slug, err := allocateSlug(r.Context(), h.FS, m.Name)
	if err != nil {
		WriteError(w, err)
		return
	}

	// 5. Create the bot user.
	botUID, err := CreateBotUser(r.Context(), h.FS, slug, m.Name, "", "pending")
	if err != nil {
		WriteError(w, err)
		return
	}

	// 6. Create the App (generates keypair, secrets, persists to Firestore + Secret Manager).
	owner := AccountRef{ID: uid, Type: AccountTypeUser}
	app, secrets, err := CreateApp(r.Context(), h.FS, h.Store, CreateAppRequest{
		Slug:               slug,
		Name:               m.Name,
		OwnerAccount:       owner,
		BotUserID:          botUID,
		WebhookURL:         m.HookAttributes.URL,
		DefaultPermissions: perms,
		DefaultEvents:      events,
	})
	if err != nil {
		// Best-effort cleanup: delete the bot user we just created.
		_, _ = h.FS.Collection("usernames").Doc(slug + "[bot]").Delete(r.Context())
		_, _ = h.FS.Collection(CollectionUsers).Doc(botUID).Delete(r.Context())
		WriteError(w, err)
		return
	}

	// 7. Stash the secrets bundle keyed by a one-time code.
	code, err := CreateManifestConversion(r.Context(), h.FS, app.AppID)
	if err != nil {
		WriteError(w, err)
		return
	}
	// Also stash the plaintext secrets under the code so hop 3 can fetch them
	// without re-running key generation. We piggyback on a sibling doc in a
	// short-lived subcollection so the Code doc itself stays small.
	if err := stashConversionSecrets(r.Context(), h.FS, code, secrets); err != nil {
		WriteError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"redirect_url": appendCodeQuery(m.RedirectURL, code),
	})

	// Keep username for downstream logging clarity.
	_ = username
}

// ExchangeManifestCode handles POST /api/v3/app-manifests/{code}/conversions.
// Looks up the code, returns the plaintext secrets bundle (one time only),
// deletes the conversion record.
func (h *Handler) ExchangeManifestCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	appID, err := ConsumeManifestConversion(r.Context(), h.FS, code)
	if err != nil || appID == "" {
		WriteError(w, ErrNotFound)
		return
	}
	app, err := GetApp(r.Context(), h.FS, appID)
	if err != nil || app == nil {
		WriteError(w, ErrNotFound)
		return
	}
	secrets, err := loadConversionSecrets(r.Context(), h.FS, code)
	if err != nil {
		WriteError(w, ErrNotFound)
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id":              app.AppID,
		"slug":            app.Slug,
		"name":            app.Name,
		"owner":           map[string]interface{}{"login": app.OwnerAccount.ID, "type": string(app.OwnerAccount.Type)},
		"client_id":       secrets.ClientID,
		"client_secret":   secrets.ClientSecret,
		"webhook_secret":  secrets.WebhookSecret,
		"pem":             secrets.PrivateKeyPEM,
		"html_url":        "", // populated when we know the public base URL; main.go sets this via an env var
		"permissions":     permissionsJSON(app.DefaultPermissions),
		"events":          app.DefaultEvents,
	})
}

// --- private helpers ------------------------------------------------------

// allocateSlug derives a unique slug from a display name. Falls back to
// appending a 4-char hex suffix on collision.
func allocateSlug(ctx context.Context, fs *firestore.Client, name string) (string, error) {
	base := slugify(name)
	if base == "" {
		base = "app"
	}
	// Truncate to fit within the username regex's 20-char limit (minus 5
	// for the "[bot]" suffix CreateBotUser will append).
	if len(base) > 15 {
		base = base[:15]
	}
	candidate := base
	for attempt := 0; attempt < 10; attempt++ {
		existing, err := GetAppBySlug(ctx, fs, candidate)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return candidate, nil
		}
		suffix := make([]byte, 2)
		_, _ = rand.Read(suffix)
		candidate = base + "-" + hex.EncodeToString(suffix)
		if len(candidate) > 20 {
			candidate = candidate[:20]
		}
	}
	return "", fmt.Errorf("could not allocate unique slug")
}

func slugify(name string) string {
	s := strings.ToLower(name)
	s = slugAllowed.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

func appendCodeQuery(redirectURL, code string) string {
	sep := "?"
	if strings.Contains(redirectURL, "?") {
		sep = "&"
	}
	return redirectURL + sep + "code=" + code
}

// --- conversion secrets stash --------------------------------------------

// conversionSecretsDoc is stored as Firestore: manifest_conversions/{code}/secrets/v1
// Same 10-minute TTL on the parent code applies (TTL deletes the code; we
// also delete the secrets sibling on ConsumeManifestConversion).
type conversionSecretsDoc struct {
	ClientID      string `firestore:"client_id"`
	ClientSecret  string `firestore:"client_secret"`
	WebhookSecret string `firestore:"webhook_secret"`
	PrivateKeyPEM string `firestore:"private_key_pem"`
}

func stashConversionSecrets(ctx context.Context, fs *firestore.Client, code string, s *AppSecrets) error {
	doc := conversionSecretsDoc{
		ClientID:      s.ClientID,
		ClientSecret:  s.ClientSecret,
		WebhookSecret: s.WebhookSecret,
		PrivateKeyPEM: s.PrivateKeyPEM,
	}
	_, err := fs.Collection(CollectionManifestConversions).Doc(code).
		Collection("secrets").Doc("v1").Create(ctx, doc)
	return err
}

func loadConversionSecrets(ctx context.Context, fs *firestore.Client, code string) (*AppSecrets, error) {
	docSnap, err := fs.Collection(CollectionManifestConversions).Doc(code).
		Collection("secrets").Doc("v1").Get(ctx)
	if err != nil {
		return nil, err
	}
	var d conversionSecretsDoc
	if err := docSnap.DataTo(&d); err != nil {
		return nil, err
	}
	// Best-effort delete after reading.
	_, _ = fs.Collection(CollectionManifestConversions).Doc(code).
		Collection("secrets").Doc("v1").Delete(ctx)
	return &AppSecrets{
		ClientID:      d.ClientID,
		ClientSecret:  d.ClientSecret,
		WebhookSecret: d.WebhookSecret,
		PrivateKeyPEM: d.PrivateKeyPEM,
	}, nil
}
```

Add the missing imports: `"context"`, `"crypto/rand"`, `"encoding/hex"`, `"fmt"`, `"cloud.google.com/go/firestore"`.

**On the secrets-stash design:** the spec §8.1 hop-2 step 9 returns `redirect_url` (which carries the code). Hop 3 then exchanges that code for the secrets. Between hop 2 and hop 3, we need to hold the secrets *somewhere* that's not visible to the world. The simplest place is a subcollection under the manifest_conversions doc — TTL on the parent doesn't auto-cascade to the subcollection, but `ConsumeManifestConversion` (called by hop 3) only finds the code if it hasn't expired, and `loadConversionSecrets` deletes the secrets after reading. There's a small window where un-exchanged secrets could linger past TTL, but they're unreachable without the code, and a follow-on can add a sweeper.

- [ ] **Step 5: Wire `RequireUID` helper on AuthHandler**

The handler calls `h.Auth.RequireUID(r)`. Check whether `internal/auth/auth.go` already exports this. If not, add it:

```go
// In internal/auth/auth.go (add near other public methods):

// RequireUID extracts and verifies the request's authentication, returning
// the UID or an error. Used by handlers that need to gate by web auth
// (Firebase ID token or DevMode mock_<uid> token).
func (a *AuthHandler) RequireUID(r *http.Request) (string, error) {
	uid := GetUID(r.Context())
	if uid != "" {
		return uid, nil
	}
	// If not yet set by middleware (e.g. handler is outside the
	// RequireWebAuth chain), parse Authorization here.
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", fmt.Errorf("missing bearer")
	}
	tok := strings.TrimSpace(h[len("Bearer "):])
	if a.DevMode && strings.HasPrefix(tok, "mock_") {
		return tok[len("mock_"):], nil
	}
	if a.Client == nil {
		return "", fmt.Errorf("auth client not configured")
	}
	t, err := a.Client.VerifyIDToken(r.Context(), tok)
	if err != nil {
		return "", err
	}
	return t.UID, nil
}
```

Adjust field names (`a.DevMode`, `a.Client`) to match the actual struct in `internal/auth/auth.go` — read the file first to confirm.

- [ ] **Step 6: Register routes**

In `internal/apps/routes.go`, inside the existing `RegisterRoutes`, add (outside the JWT-authed `/api/v3/app` group):

```go
// Plan 4: manifest registration flow.
r.Post("/api/v3/settings/apps/manifest-conversions", h.CreateManifestApp)
r.Post("/api/v3/app-manifests/{code}/conversions", h.ExchangeManifestCode)
```

These endpoints have their own auth: `CreateManifestApp` calls `h.Auth.RequireUID` internally; `ExchangeManifestCode` is unauthenticated (the code itself is the credential).

- [ ] **Step 7: Update main.go**

In `main.go`, the `apps.NewHandler` call needs the new `authHandler` argument:

```go
appsHandler := apps.NewHandler(firestoreClient, appsSecretStore, appsJWTVerifier, authHandler)
```

- [ ] **Step 8: Fix call sites in tests**

Find all `apps.NewHandler` test call sites and add `nil` (or the test's auth handler) as the 4th argument:

```bash
grep -rn "NewHandler(fs, " internal/apps/ | grep -v "NewV3Handler\|NewJWTVerifier\|NewMemoryEnqueuer\|NewWebhookSecretCache\|NewBotIdentityCache\|NewDispatcherHandler\|NewRealSecretStore\|NewMemorySecretStore"
```

For each, append `nil` if the test doesn't need real web auth.

- [ ] **Step 9: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/... 2>&1 | tail -30
go build ./...
go vet ./...

git add internal/apps/manifest_handlers.go internal/apps/manifest_handlers_test.go internal/apps/handlers.go internal/apps/routes.go internal/auth/auth.go main.go internal/apps/handlers_test.go internal/apps/e2e_test.go internal/apps/e2e_webhook_test.go internal/apps/e2e_loop_prevention_test.go
git commit -m "feat(apps): manifest hop-2 + hop-3 endpoints (web auth + code exchange)"
```

Report Status, commit SHA, test output. Flag any test call sites you had to touch.

---

## Task 3: Installation flow endpoint

**Files:**
- Create: `internal/apps/installations_handler.go`
- Create: `internal/apps/installations_handler_test.go`
- Modify: `internal/apps/routes.go` — register one new route

`POST /api/v3/user/installations` is the SPA-driven endpoint that creates an installation when a user clicks "Install" on an App's install page. It also fires the `installation:created` event (which a future Plan 4-follow-on for App-side install-event wiring can consume — for now, the App might already have a webhook URL set so the event just goes nowhere harmful).

- [ ] **Step 1: Write failing test**

Create `internal/apps/installations_handler_test.go`:

```go
package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestCreateUserInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	suffix := randHex(4)
	uid := "ui-uid-" + suffix
	username := "ui-user-" + suffix
	if err := db.RegisterUsername(ctx, fs, uid, username, uid+"@test"); err != nil {
		t.Fatalf("RegisterUsername: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(username).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	// Seed an App owned by a different user (the app's owner — doesn't
	// matter for install) so we have an app_id to install.
	store := NewMemorySecretStore()
	botUID, _ := CreateBotUser(ctx, fs, "ui-app-"+suffix, "UI App", "", "pending")
	app, _, err := CreateApp(ctx, fs, store, CreateAppRequest{
		Slug:       "ui-app-" + suffix,
		Name:       "UI App",
		OwnerAccount: AccountRef{ID: "other-uid-" + suffix, Type: AccountTypeUser},
		BotUserID:  botUID,
		WebhookURL: "https://example.test/hook",
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc("ui-app-" + suffix + "[bot]").Delete(cctx)
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, store, NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	body, _ := json.Marshal(map[string]interface{}{
		"app_id":              app.AppID,
		"repository_selection": "all",
	})
	req := httptest.NewRequest("POST", "/api/v3/user/installations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	installID, _ := resp["id"].(string)
	if installID == "" {
		t.Fatal("response missing id")
	}
	t.Cleanup(func() {
		_, _ = fs.Collection(CollectionInstallations).Doc(installID).Delete(context.Background())
	})

	// Sanity: GetInstallation returns it with the user's account.
	got, _ := GetInstallation(ctx, fs, installID)
	if got == nil {
		t.Fatal("installation not persisted")
	}
	if got.Account.ID != uid {
		t.Errorf("account.id = %q, want %q (the authed user)", got.Account.ID, uid)
	}
}

func TestCreateUserInstallation_AppNotFound(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	uid := "ui-nf-" + randHex(4)
	_ = db.RegisterUsername(ctx, fs, uid, uid, uid+"@test")
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(uid).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	h := NewHandler(fs, NewMemorySecretStore(), NewJWTVerifier(fs, 60*time.Second), authH)
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	body, _ := json.Marshal(map[string]interface{}{
		"app_id":              "no-such-app",
		"repository_selection": "all",
	})
	req := httptest.NewRequest("POST", "/api/v3/user/installations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rr.Code)
	}
}
```

- [ ] **Step 2: Confirm fail**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateUserInstallation -v
```

Expected: route not found (404) on both subtests.

- [ ] **Step 3: Implementation**

Create `internal/apps/installations_handler.go`:

```go
package apps

import (
	"encoding/json"
	"net/http"
	"time"
)

// CreateUserInstallation handles POST /api/v3/user/installations. The
// authenticated user (Firebase web auth) installs an App onto their account.
//
// Request body:
//   {
//     "app_id": "<app_id>",
//     "repository_selection": "all" | "selected",
//     "repository_ids": ["<owner>_<repo>", ...]   // required when selection == "selected"
//   }
func (h *Handler) CreateUserInstallation(w http.ResponseWriter, r *http.Request) {
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}

	var req struct {
		AppID               string   `json:"app_id"`
		RepositorySelection string   `json:"repository_selection"`
		RepositoryIDs       []string `json:"repository_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}
	if req.AppID == "" {
		WriteError(w, ErrUnprocessable)
		return
	}

	app, err := GetApp(r.Context(), h.FS, req.AppID)
	if err != nil || app == nil {
		WriteError(w, ErrNotFound)
		return
	}

	inst, err := CreateInstallation(r.Context(), h.FS, CreateInstallationRequest{
		AppID:               app.AppID,
		Account:             AccountRef{ID: uid, Type: AccountTypeUser},
		RepositorySelection: req.RepositorySelection,
		RepositoryIDs:       req.RepositoryIDs,
		Permissions:         app.DefaultPermissions,
		Events:              app.DefaultEvents,
	})
	if err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}

	// Fire installation:created event (best-effort; Plan 3's Fire is a no-op
	// when h.Events is zero-valued, so existing tests don't break).
	now := time.Now().UTC()
	_ = now
	Fire(r.Context(), h.Events, InstallationPayload{
		Action:  "created",
		AppID:   app.AppID,
		Account: AccountRef{ID: uid, Type: AccountTypeUser},
		Sender:  SenderRef{Login: uid, Type: "User"},
	})

	WriteJSON(w, http.StatusCreated, installationJSON(inst))
}
```

**Wait — `Handler` doesn't have an `Events` field yet.** Add it in `handlers.go` (same way `V3Handler.Events` was added in Plan 3 Task 10):

```go
type Handler struct {
	FS     *firestore.Client
	Store  SecretStore
	JWT    *JWTVerifier
	Auth   *auth.AuthHandler
	Events FireDeps
}
```

Populate it in `main.go` after construction (mirror the v3Handler.Events line):

```go
appsHandler.Events = fireDeps
```

- [ ] **Step 4: Register the route**

In `internal/apps/routes.go`:

```go
r.Post("/api/v3/user/installations", h.CreateUserInstallation)
```

- [ ] **Step 5: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestCreateUserInstallation -v
go vet ./internal/apps/...
go build ./...

git add internal/apps/installations_handler.go internal/apps/installations_handler_test.go internal/apps/handlers.go internal/apps/routes.go main.go
git commit -m "feat(apps): POST /api/v3/user/installations + installation:created event"
```

Report Status, commit SHA, test output.

---

## Task 4: Bot impersonation guards (spec §9.4)

**Files:**
- Modify: `internal/db/db.go` — reject `type:"Bot"` users from PAT creation paths
- Modify: `internal/auth/auth.go` — reject bot users from any web-auth context

Spec §9.4: bot users cannot be impersonated. Defense in depth.

- [ ] **Step 1: Read existing PAT code**

```bash
grep -n "func.*GeneratePAT\|func.*VerifyPAT" internal/db/db.go
```

The existing `GeneratePAT(ctx, fs, uid, name)` writes a PAT bound to a UID. If `uid` refers to a `type:"Bot"` user, we must reject before writing.

- [ ] **Step 2: Add a helper that rejects bots**

In `internal/db/db.go`, add:

```go
// RejectBotUID returns an error when uid refers to a synthetic bot user.
// Used by paths that mint credentials (PAT generation, login) to prevent
// impersonation of bot accounts.
func RejectBotUID(ctx context.Context, client *firestore.Client, uid string) error {
	doc, err := client.Collection("users").Doc(uid).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil // unknown UID — let the caller decide what to do
		}
		return err
	}
	typeVal, _ := doc.DataAt("type")
	if t, ok := typeVal.(string); ok && t == "Bot" {
		return fmt.Errorf("bot user cannot perform this action")
	}
	return nil
}
```

- [ ] **Step 3: Wire RejectBotUID into GeneratePAT**

In `GeneratePAT`, add the check at the top after the nil-client guard:

```go
if err := RejectBotUID(ctx, client, uid); err != nil {
    return "", err
}
```

- [ ] **Step 4: Add an auth-layer guard**

In `internal/auth/auth.go`'s middleware (the one that sets UID in context after token verification), add a similar guard so a stolen Firebase token whose UID happens to be a bot UID is also rejected. Find the existing token-verify code in `RequireWebAuth` (or `OptionalWebAuth`) and after the UID is established, add:

```go
if err := db.RejectBotUID(r.Context(), a.fs, uid); err != nil {
    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    return
}
```

(Adjust field references to match the actual `AuthHandler` struct.)

- [ ] **Step 5: Test the guard**

Add a test in `internal/db/db_test.go`:

```go
func TestRejectBotUID(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	suffix := hex.EncodeToString([]byte{0xab, 0xcd})
	// Seed a bot user.
	botUID := "bot_test_" + suffix
	_, err := fs.Collection("users").Doc(botUID).Set(ctx, map[string]interface{}{
		"username": "test-bot-" + suffix + "[bot]",
		"type":     "Bot",
	})
	if err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("users").Doc(botUID).Delete(context.Background())
	})

	if err := RejectBotUID(ctx, fs, botUID); err == nil {
		t.Error("expected error for bot UID")
	}

	// Seed a normal user.
	humanUID := "human_test_" + suffix
	_, _ = fs.Collection("users").Doc(humanUID).Set(ctx, map[string]interface{}{
		"username": "test-human-" + suffix,
		"type":     "User",
	})
	t.Cleanup(func() {
		_, _ = fs.Collection("users").Doc(humanUID).Delete(context.Background())
	})

	if err := RejectBotUID(ctx, fs, humanUID); err != nil {
		t.Errorf("unexpected error for human UID: %v", err)
	}
}

func TestGeneratePAT_RejectsBot(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	botUID := "bot_pat_" + hex.EncodeToString([]byte{0xef, 0x12})
	_, _ = fs.Collection("users").Doc(botUID).Set(ctx, map[string]interface{}{
		"username": "pat-bot[bot]",
		"type":     "Bot",
	})
	t.Cleanup(func() {
		_, _ = fs.Collection("users").Doc(botUID).Delete(context.Background())
	})

	if _, err := GeneratePAT(ctx, fs, botUID, "test pat"); err == nil {
		t.Error("expected error when generating PAT for a bot user")
	}
}
```

Add `"os"`, `"encoding/hex"`, `"context"`, `"testing"` imports to `db_test.go` if not present.

- [ ] **Step 6: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/db/... ./internal/auth/... -v 2>&1 | tail -20
go vet ./...
go build ./...

git add internal/db/db.go internal/db/db_test.go internal/auth/auth.go
git commit -m "feat(db,auth): reject bot users from PAT creation + web-auth paths (§9.4)"
```

Report Status, commit SHA.

---

## Task 5: SPA — manifest confirmation page

**Files:**
- Create: `frontend/src/pages/SettingsAppsNew.jsx`
- Create: `frontend/src/utils/manifestUrl.js`
- Modify: `frontend/src/App.jsx` — add route for `apps_new`
- Modify: `frontend/src/apiClient.js` — add `submitManifestConversion`

Hop 1 of the manifest flow: the agent redirects the user's browser to `/settings/apps/new?manifest=<URL-encoded JSON>`. The SPA renders a confirmation card with the App's name, permissions, events, and webhook URL. On confirm, the SPA POSTs to the hop-2 endpoint and redirects the browser to the `redirect_url` returned by the backend.

- [ ] **Step 1: Add the URL parser helper**

Create `frontend/src/utils/manifestUrl.js`:

```js
// Parses the ?manifest= query parameter into a manifest object.
// The agent URL-encodes a JSON blob; we decode once.
export function parseManifestFromURL(searchString) {
  const params = new URLSearchParams(searchString);
  const raw = params.get('manifest');
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}
```

- [ ] **Step 2: Add apiClient method**

In `frontend/src/apiClient.js`, add to the exported object:

```js
async submitManifestConversion(manifest) {
  // POST /api/v3/settings/apps/manifest-conversions
  // Requires the user's Firebase ID token in Authorization (apiClient adds it automatically).
  return this.post('/api/v3/settings/apps/manifest-conversions', { manifest });
},
```

If `apiClient.js` doesn't have a generic `post` helper, add this method using the same auth+fetch pattern as the other apiClient methods. Read the existing methods (`get`, `patch` already exist per Plan 1's session-start status) and follow their shape exactly.

- [ ] **Step 3: SPA page component**

Create `frontend/src/pages/SettingsAppsNew.jsx`:

```jsx
import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';
import { parseManifestFromURL } from '../utils/manifestUrl';

export default function SettingsAppsNew({ user, onNavigate }) {
  const [manifest, setManifest] = useState(null);
  const [error, setError] = useState(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    const m = parseManifestFromURL(window.location.search);
    if (!m) {
      setError('Missing or invalid manifest in URL.');
      return;
    }
    setManifest(m);
  }, []);

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const resp = await apiClient.submitManifestConversion(manifest);
      // Redirect the browser to the agent's redirect_url (which carries ?code=).
      window.location.assign(resp.redirect_url);
    } catch (e) {
      setError(e.message || 'Failed to create app');
      setSubmitting(false);
    }
  };

  if (error && !manifest) {
    return (
      <div style={{ padding: '2rem', color: '#fca5a5' }}>
        <h2>Invalid request</h2>
        <p>{error}</p>
        <button onClick={() => onNavigate('dashboard')}>Back to dashboard</button>
      </div>
    );
  }
  if (!manifest) {
    return <div style={{ padding: '2rem', color: '#94a3b8' }}>Loading manifest…</div>;
  }

  return (
    <div style={{ maxWidth: '640px', margin: '2rem auto', padding: '2rem',
                  background: '#0f172a', border: '1px solid #334155', borderRadius: '12px',
                  color: '#e2e8f0', fontFamily: 'system-ui' }}>
      <h2 style={{ marginTop: 0 }}>Register GitHub App</h2>
      <p>
        <strong>{manifest.name}</strong> is requesting to register an App on your account
        <span style={{ color: '#94a3b8' }}> ({user?.email || 'logged in user'})</span>.
      </p>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>App URL</h3>
      <code style={{ color: '#a5b4fc' }}>{manifest.url}</code>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>Webhook URL</h3>
      <code style={{ color: '#a5b4fc' }}>{manifest.hook_attributes?.url || '(none)'}</code>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>Permissions</h3>
      <ul style={{ paddingLeft: '1.25rem' }}>
        {Object.entries(manifest.default_permissions || {}).map(([scope, level]) => (
          <li key={scope}>
            <code>{scope}</code>: <span style={{ color: '#fde68a' }}>{level}</span>
          </li>
        ))}
      </ul>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>Events</h3>
      <ul style={{ paddingLeft: '1.25rem' }}>
        {(manifest.default_events || []).map(e => <li key={e}><code>{e}</code></li>)}
      </ul>

      {error && (
        <div style={{ marginTop: '1rem', padding: '0.75rem', background: '#7f1d1d',
                      color: '#fecaca', borderRadius: '6px' }}>
          {error}
        </div>
      )}

      <div style={{ marginTop: '1.5rem', display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={handleConfirm}
          disabled={submitting}
          style={{ background: '#22c55e', color: 'black', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px', fontWeight: 600,
                   cursor: submitting ? 'wait' : 'pointer' }}>
          {submitting ? 'Creating…' : `Create App on behalf of ${user?.email || 'me'}`}
        </button>
        <button
          onClick={() => onNavigate('dashboard')}
          disabled={submitting}
          style={{ background: '#334155', color: '#e2e8f0', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px' }}>
          Cancel
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Wire route in App.jsx**

Read `frontend/src/App.jsx` to find the pathname-to-page-name resolver. Add a new branch matching `/settings/apps/new` → page name `'apps_new'`. Then add to the renderView switch:

```jsx
case 'apps_new':
  return <SettingsAppsNew user={user} onNavigate={navigate} />;
```

Import:

```jsx
import SettingsAppsNew from './pages/SettingsAppsNew';
```

- [ ] **Step 5: Commit**

```bash
cd frontend && npm run build 2>&1 | tail -10 && cd ..

git add frontend/src/pages/SettingsAppsNew.jsx frontend/src/utils/manifestUrl.js frontend/src/App.jsx frontend/src/apiClient.js
git commit -m "feat(frontend): manifest confirmation page (hop 1 of App registration)"
```

Report Status, commit SHA, build output.

---

## Task 6: SPA — App install page

**Files:**
- Create: `frontend/src/pages/SettingsAppsInstall.jsx`
- Modify: `frontend/src/App.jsx` — add route for `apps_install`
- Modify: `frontend/src/apiClient.js` — add `getApp(slug)` + `installApp`

After registration, the user navigates to `/settings/apps/{slug}/installations/new`. The SPA fetches the App by slug (so it can show the App's name + permissions), lets the user choose `all` vs `selected` repos, and POSTs to `/api/v3/user/installations`.

- [ ] **Step 1: Add apiClient methods**

In `frontend/src/apiClient.js`:

```js
async getAppBySlug(slug) {
  // No GET-by-slug endpoint exists yet. For Plan 4 MVP, the install page
  // can call GET /api/v3/app (App JWT required) — but the SPA doesn't have
  // a JWT, so it can't. WORKAROUND: add a small unauthenticated read endpoint
  // GET /api/v3/apps/{slug}/public that returns just the public fields
  // (name, slug, permissions, events). Track as a follow-on if not done now.
  //
  // Alternative for MVP: skip the App info fetch — show only the slug in
  // the install UI. User won't see the App's name/permissions until install.
  return this.get(`/api/v3/apps/${slug}/public`);
},

async installApp({ appId, repositorySelection, repositoryIds }) {
  return this.post('/api/v3/user/installations', {
    app_id: appId,
    repository_selection: repositorySelection,
    repository_ids: repositoryIds || [],
  });
},

async listMyRepos() {
  // Existing endpoint — verify the path against your apiClient's repo-listing call.
  return this.get('/api/repos');
},
```

For MVP, we can skip the App-name fetch and just show the slug. To do it properly, add a small public-read endpoint:

In `internal/apps/manifest_handlers.go`, add:

```go
// GetAppPublic handles GET /api/v3/apps/{slug}/public — returns just the
// fields safe for an unauthenticated install-page preview.
func (h *Handler) GetAppPublic(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	app, err := GetAppBySlug(r.Context(), h.FS, slug)
	if err != nil || app == nil {
		WriteError(w, ErrNotFound)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id":          app.AppID,
		"slug":        app.Slug,
		"name":        app.Name,
		"permissions": permissionsJSON(app.DefaultPermissions),
		"events":      app.DefaultEvents,
	})
}
```

Register in `routes.go`:

```go
r.Get("/api/v3/apps/{slug}/public", h.GetAppPublic)
```

- [ ] **Step 2: SPA install page**

Create `frontend/src/pages/SettingsAppsInstall.jsx`:

```jsx
import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';

export default function SettingsAppsInstall({ user, slug, onNavigate }) {
  const [app, setApp] = useState(null);
  const [repos, setRepos] = useState([]);
  const [selection, setSelection] = useState('all');
  const [pickedRepos, setPickedRepos] = useState(new Set());
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    apiClient.getAppBySlug(slug).then(setApp).catch(e => setError(e.message));
    apiClient.listMyRepos().then(r => setRepos(r || [])).catch(() => setRepos([]));
  }, [slug]);

  const toggleRepo = (repoId) => {
    const next = new Set(pickedRepos);
    if (next.has(repoId)) next.delete(repoId);
    else next.add(repoId);
    setPickedRepos(next);
  };

  const handleInstall = async () => {
    if (!app) return;
    if (selection === 'selected' && pickedRepos.size === 0) {
      setError('Pick at least one repository.');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await apiClient.installApp({
        appId: app.id,
        repositorySelection: selection,
        repositoryIds: selection === 'selected' ? Array.from(pickedRepos) : [],
      });
      onNavigate('apps_list');
    } catch (e) {
      setError(e.message || 'Failed to install app');
      setSubmitting(false);
    }
  };

  if (!app && !error) {
    return <div style={{ padding: '2rem', color: '#94a3b8' }}>Loading…</div>;
  }
  if (error && !app) {
    return <div style={{ padding: '2rem', color: '#fca5a5' }}>{error}</div>;
  }

  return (
    <div style={{ maxWidth: '640px', margin: '2rem auto', padding: '2rem',
                  background: '#0f172a', border: '1px solid #334155', borderRadius: '12px',
                  color: '#e2e8f0' }}>
      <h2 style={{ marginTop: 0 }}>Install {app.name}</h2>
      <p>
        Repository access:
      </p>
      <label style={{ display: 'block', marginBottom: '0.5rem' }}>
        <input
          type="radio" name="sel" value="all"
          checked={selection === 'all'}
          onChange={() => setSelection('all')}
        /> All repositories
      </label>
      <label style={{ display: 'block', marginBottom: '0.5rem' }}>
        <input
          type="radio" name="sel" value="selected"
          checked={selection === 'selected'}
          onChange={() => setSelection('selected')}
        /> Only select repositories
      </label>

      {selection === 'selected' && (
        <div style={{ marginTop: '1rem', maxHeight: '300px', overflowY: 'auto',
                      border: '1px solid #334155', padding: '0.5rem', borderRadius: '6px' }}>
          {repos.map(r => {
            const repoId = `${r.owner}_${r.name}`;
            return (
              <label key={repoId} style={{ display: 'block', padding: '0.25rem 0' }}>
                <input
                  type="checkbox"
                  checked={pickedRepos.has(repoId)}
                  onChange={() => toggleRepo(repoId)}
                /> {r.owner}/{r.name}
              </label>
            );
          })}
        </div>
      )}

      {error && (
        <div style={{ marginTop: '1rem', padding: '0.75rem', background: '#7f1d1d',
                      color: '#fecaca', borderRadius: '6px' }}>
          {error}
        </div>
      )}

      <div style={{ marginTop: '1.5rem', display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={handleInstall}
          disabled={submitting}
          style={{ background: '#22c55e', color: 'black', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px', fontWeight: 600,
                   cursor: submitting ? 'wait' : 'pointer' }}>
          {submitting ? 'Installing…' : 'Install'}
        </button>
        <button
          onClick={() => onNavigate('apps_list')}
          style={{ background: '#334155', color: '#e2e8f0', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px' }}>
          Cancel
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Wire route**

Update `App.jsx` to match `/settings/apps/{slug}/installations/new` → page `apps_install` with `params: { slug }`. Add to renderView switch.

- [ ] **Step 4: Build + commit**

```bash
cd frontend && npm run build 2>&1 | tail -10 && cd ..

git add frontend/src/pages/SettingsAppsInstall.jsx frontend/src/App.jsx frontend/src/apiClient.js internal/apps/manifest_handlers.go internal/apps/routes.go
git commit -m "feat(frontend): App install page + GET /api/v3/apps/{slug}/public"
```

---

## Task 7: SPA — Apps list page

**Files:**
- Create: `frontend/src/pages/SettingsApps.jsx`
- Modify: `frontend/src/App.jsx` — add route `apps_list`
- Modify: `frontend/src/apiClient.js` — `listMyApps` + `listMyInstallations`
- Add: backend endpoint `GET /api/v3/user/apps` + `GET /api/v3/user/installations` (or reuse existing handlers if applicable)

The user-facing read-only list of: (a) Apps the user owns + (b) Apps installed on the user's account.

- [ ] **Step 1: Backend list endpoints**

In `internal/apps/manifest_handlers.go` (or a new `user_endpoints.go`), add:

```go
func (h *Handler) ListMyApps(w http.ResponseWriter, r *http.Request) {
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}
	iter := h.FS.Collection(CollectionApps).Where("owner_account.id", "==", uid).Documents(r.Context())
	defer iter.Stop()
	out := []map[string]interface{}{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var a App
		if err := doc.DataTo(&a); err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":   a.AppID,
			"slug": a.Slug,
			"name": a.Name,
		})
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) ListMyInstallations(w http.ResponseWriter, r *http.Request) {
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}
	iter := h.FS.Collection(CollectionInstallations).Where("account.id", "==", uid).Documents(r.Context())
	defer iter.Stop()
	out := []map[string]interface{}{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var inst Installation
		if err := doc.DataTo(&inst); err != nil {
			continue
		}
		app, _ := GetApp(r.Context(), h.FS, inst.AppID)
		appName := ""
		if app != nil {
			appName = app.Name
		}
		out = append(out, map[string]interface{}{
			"id":       inst.InstallationID,
			"app_id":   inst.AppID,
			"app_name": appName,
		})
	}
	WriteJSON(w, http.StatusOK, out)
}
```

Register routes:

```go
r.Get("/api/v3/user/apps", h.ListMyApps)
r.Get("/api/v3/user/installations", h.ListMyInstallations)
```

- [ ] **Step 2: apiClient methods + SPA page**

Add to apiClient:

```js
async listMyApps() {
  return this.get('/api/v3/user/apps');
},
async listMyInstallations() {
  return this.get('/api/v3/user/installations');
},
```

Create `frontend/src/pages/SettingsApps.jsx`:

```jsx
import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';

export default function SettingsApps({ user, onNavigate }) {
  const [owned, setOwned] = useState([]);
  const [installed, setInstalled] = useState([]);

  useEffect(() => {
    apiClient.listMyApps().then(setOwned).catch(() => setOwned([]));
    apiClient.listMyInstallations().then(setInstalled).catch(() => setInstalled([]));
  }, []);

  return (
    <div style={{ maxWidth: '720px', margin: '2rem auto', padding: '2rem', color: '#e2e8f0' }}>
      <h2>GitHub Apps</h2>

      <section>
        <h3>Owned by me</h3>
        {owned.length === 0 ? (
          <p style={{ color: '#94a3b8' }}>No Apps yet.</p>
        ) : (
          <ul>
            {owned.map(a => (
              <li key={a.id}>
                <a href={`/settings/apps/${a.slug}/installations/new`}>{a.name}</a>
                <span style={{ color: '#94a3b8' }}> ({a.slug})</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section style={{ marginTop: '2rem' }}>
        <h3>Installed on my account</h3>
        {installed.length === 0 ? (
          <p style={{ color: '#94a3b8' }}>No installations yet.</p>
        ) : (
          <ul>
            {installed.map(i => (
              <li key={i.id}>{i.app_name || i.app_id}</li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
```

Wire `apps_list` route in App.jsx + the renderView switch.

- [ ] **Step 3: Build + commit**

```bash
cd frontend && npm run build 2>&1 | tail -10 && cd ..

git add frontend/src/pages/SettingsApps.jsx frontend/src/App.jsx frontend/src/apiClient.js internal/apps/manifest_handlers.go internal/apps/routes.go
git commit -m "feat(frontend): Apps list page + GET /user/apps + GET /user/installations"
```

---

## Task 8: End-to-end manifest flow test

**Files:**
- Create: `internal/apps/e2e_manifest_test.go`

Full simulation of a real agent's registration flow: a fake Claude server initiates the manifest flow, the test mints a web-auth token to act as the consenting user, exercises hops 2 + 3, and asserts the bundle returned by hop 3 is usable to sign a JWT + mint an installation token + use it on a v3 endpoint.

- [ ] **Step 1: Write the test**

Create `internal/apps/e2e_manifest_test.go`:

```go
package apps_test

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
	"github.com/golang-jwt/jwt/v4"

	v3 "gitbucket/internal/api/v3"
	"gitbucket/internal/apps"
	"gitbucket/internal/auth"
	"gitbucket/internal/db"
)

func TestPlan4FullManifestFlow(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	// Seed a human user (the registering owner).
	suffix := randHexExt(4)
	uid := "owner-" + suffix
	username := "owner-" + suffix
	if err := db.RegisterUsername(ctx, fs, uid, username, uid+"@test"); err != nil {
		t.Fatalf("RegisterUsername: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fs.Collection("usernames").Doc(username).Delete(context.Background())
		_, _ = fs.Collection("users").Doc(uid).Delete(context.Background())
	})

	authH := auth.NewAuthHandler(true, nil, fs)
	store := apps.NewMemorySecretStore()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	appsH := apps.NewHandler(fs, store, jwtV, authH)

	r := chi.NewRouter()
	apps.RegisterRoutes(r, appsH)

	v3H := v3.NewV3Handler(fs, nil, "https://test.gb")
	v3.RegisterV3Routes(r, v3H)

	// Hop 2: submit manifest as the logged-in user.
	manifest := map[string]interface{}{
		"name":             "FakeClaude " + suffix,
		"url":              "https://claude.test",
		"hook_attributes":  map[string]interface{}{"url": "https://claude.test/webhook"},
		"redirect_url":     "https://claude.test/setup/callback",
		"default_permissions": map[string]interface{}{
			"contents":      "write",
			"pull_requests": "write",
			"metadata":      "read",
		},
		"default_events": []string{"pull_request", "push"},
	}
	body, _ := json.Marshal(map[string]interface{}{"manifest": manifest})
	req := httptest.NewRequest("POST", "/api/v3/settings/apps/manifest-conversions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mock_"+uid)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("hop2: %d %s", rr.Code, rr.Body.String())
	}
	var hop2 map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &hop2)
	redirectURL, _ := hop2["redirect_url"].(string)
	parts := strings.SplitN(redirectURL, "code=", 2)
	if len(parts) != 2 {
		t.Fatalf("no code in redirect_url: %s", redirectURL)
	}
	code := parts[1]

	// Hop 3: exchange the code as the agent would.
	rr3 := httptest.NewRecorder()
	r.ServeHTTP(rr3, httptest.NewRequest("POST", "/api/v3/app-manifests/"+code+"/conversions", nil))
	if rr3.Code != http.StatusOK {
		t.Fatalf("hop3: %d %s", rr3.Code, rr3.Body.String())
	}
	var bundle map[string]interface{}
	_ = json.Unmarshal(rr3.Body.Bytes(), &bundle)
	pemStr, _ := bundle["pem"].(string)
	appID, _ := bundle["id"].(string)
	slug, _ := bundle["slug"].(string)
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(apps.CollectionApps).Doc(appID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		docs, _ := fs.Collection(apps.CollectionUsers).Where("username", "==", slug+"[bot]").Documents(cctx).GetAll()
		for _, d := range docs {
			_, _ = d.Ref.Delete(cctx)
		}
	})

	// Install the App as the same user.
	installBody, _ := json.Marshal(map[string]interface{}{
		"app_id":              appID,
		"repository_selection": "all",
	})
	installReq := httptest.NewRequest("POST", "/api/v3/user/installations", bytes.NewReader(installBody))
	installReq.Header.Set("Authorization", "Bearer mock_"+uid)
	installRR := httptest.NewRecorder()
	r.ServeHTTP(installRR, installReq)
	if installRR.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", installRR.Code, installRR.Body.String())
	}
	var installResp map[string]interface{}
	_ = json.Unmarshal(installRR.Body.Bytes(), &installResp)
	installationID, _ := installResp["id"].(string)
	t.Cleanup(func() {
		_, _ = fs.Collection(apps.CollectionInstallations).Doc(installationID).Delete(context.Background())
	})

	// Use the App PEM to sign a JWT, then exchange for an installation token.
	block, _ := pem.Decode([]byte(pemStr))
	priv, _ := x509.ParsePKCS1PrivateKey(block.Bytes)
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": appID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}
	jwtStr, _ := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(priv)

	mintReq := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+installationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	mintReq.Header.Set("Authorization", "Bearer "+jwtStr)
	mintRR := httptest.NewRecorder()
	r.ServeHTTP(mintRR, mintReq)
	if mintRR.Code != http.StatusCreated {
		t.Fatalf("mint: %d %s", mintRR.Code, mintRR.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(mintRR.Body.Bytes(), &minted)
	tok, _ := minted["token"].(string)
	if !strings.HasPrefix(tok, "ghs_") {
		t.Fatalf("bad token: %q", tok)
	}

	// Use the token on an installation-authed endpoint — sanity probe.
	probeReq := httptest.NewRequest("GET", "/api/v3/_ping", nil)
	probeReq.Header.Set("Authorization", "Bearer "+tok)
	probeRR := httptest.NewRecorder()
	r.ServeHTTP(probeRR, probeReq)
	if probeRR.Code != http.StatusOK {
		t.Fatalf("ping: %d %s", probeRR.Code, probeRR.Body.String())
	}
}

func randHexExt(n int) string {
	// Reuse-or-duplicate the helper from earlier tests.
	b := make([]byte, n)
	for i := range b {
		b[i] = "0123456789abcdef"[i%16]
	}
	return string(b)
}
```

(`randHexExt` is a deterministic-but-acceptable helper for test data; replace with a real `crypto/rand.Read` if you prefer.)

- [ ] **Step 2: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestPlan4FullManifestFlow -v
git add internal/apps/e2e_manifest_test.go
git commit -m "test(apps): plan-4 end-to-end manifest registration flow"
```

Report Status, commit SHA, test output.

---

## Self-Review

### Spec coverage

Plan 4 covers spec §8 + §9.4:

- §8.1 hop 1 (manifest landing page) — Task 5 (frontend SPA route + page)
- §8.1 hop 2 (manifest conversion endpoint) — Tasks 1 + 2 (data layer + backend handler)
- §8.1 hop 3 (code exchange endpoint) — Task 2 (same file, second handler)
- §8.2 installation flow — Task 3 (backend) + Task 6 (frontend install page)
- §8.3 who can register/install — Implemented via `RequireUID` (any authenticated GitBucket user can register; only the account owner can install on their own account)
- §8.4 UX surface for MVP — Tasks 5, 6, 7 (three SPA pages)
- §9.4 bot users cannot be impersonated — Task 4

### Placeholder scan

- Task 1 Step 1 test contains a `backdateConversionForTest` stub with explicit replacement instructions.
- Task 6 has a "WORKAROUND" comment in apiClient — the GetAppPublic endpoint is added in the same task to address it.
- Task 8's `randHexExt` is a stand-in helper with explicit note about using crypto/rand for real.

All other steps contain complete, executable code.

### Type consistency

- `ManifestConversion`, `CollectionManifestConversions`, `ManifestConversionTTL` — defined in Task 1, used in Tasks 2 + 8
- `Handler.Auth`, `Handler.Events` — added incrementally in Tasks 2 + 3, used consistently
- `Fire(ctx, deps, InstallationPayload{...})` — uses Plan 3's events package; signature matches existing
- `RejectBotUID` — added in Task 4, called from PAT + auth paths

### Scope check

Plan 4 produces a working end-to-end App registration + install flow: a real agent like Claude Code can point at the GitBucket instance, walk the 3-hop dance, install onto a repo, and start operating. The end-to-end test (Task 8) simulates this exact flow.

### Operational notes

- New Firestore TTL config: `manifest_conversions` collection on `expires_at`. Documented in File Structure section.
- No new env vars beyond what Plans 1-3 added.
- No new dependencies.
- `PUBLIC_BASE_URL` from Plan 2/3 stays load-bearing; the manifest hop-3 response's `html_url` field is empty in this MVP and can be populated as a small follow-on (read from env or config).

---

## Execution notes

- Stack this plan on `feature/github-app-emulation-webhook-engine` (Plan 3) — Plan 3 is in PR but not merged. Branch name suggestion: `feature/github-app-emulation-manifest-flow`. If Plans 1-3 merge to main first, rebase onto main.
- Firestore emulator on `localhost:8084` is required for integration tests.
- The Handler signature change in Task 2 (adding `*auth.AuthHandler` parameter) is a breaking change. Update ALL test call sites in the same task — `grep -rn "apps.NewHandler" internal/ main.go` finds them.
- Frontend changes ship a working SPA. Run `cd frontend && npm run build` after each frontend task to catch syntax / import errors early.
