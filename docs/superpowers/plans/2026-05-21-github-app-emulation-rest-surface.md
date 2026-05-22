# GitHub App Emulation — Plan 2: GitHub-Shape REST Surface

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the GitHub-compatible REST endpoints that installation-token-authenticated Apps will use to read repository metadata, browse files/refs/trees, and read/create/update pull requests. Adds the `internal/api/v3/v3fmt/` formatter package (pure functions translating internal types to GitHub's exact JSON shape), a `URLBuilder` for constructing GitHub-style URLs, and a stable `int64` ID derivation helper. Also fixes the Plan 1 placeholder where the token-mint response returned an empty `repositories[]` array.

**Architecture:** A new chi subrouter mounted at `/api/v3/{owner}/{repo}/...` (and `/api/v3/repos/...`) behind `RequireInstallationToken` (the middleware shipped in Plan 1). Handlers live under `internal/api/v3/` and call existing data-layer functions in `internal/db/` and `internal/api/` (with thin wrappers when needed to skip Firebase-auth-coupled authorization). All HTTP response shaping is delegated to `internal/api/v3/v3fmt/`, a leaf package of pure formatter functions that are golden-fixture tested.

**Tech Stack:** Same as Plan 1 — Go, chi v5, Firestore. No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-05-20-github-app-emulation-design.md` §6
**Builds on:** `docs/superpowers/plans/2026-05-20-github-app-emulation-foundation.md`

**Scope of Plan 2:** Spec §6 endpoints that have existing backend services:

```
GET    /api/v3/repos/{owner}/{repo}
GET    /api/v3/repos/{owner}/{repo}/contents/{path}
GET    /api/v3/repos/{owner}/{repo}/git/ref/{ref}
GET    /api/v3/repos/{owner}/{repo}/git/trees/{sha}
GET    /api/v3/repos/{owner}/{repo}/pulls
GET    /api/v3/repos/{owner}/{repo}/pulls/{number}
POST   /api/v3/repos/{owner}/{repo}/pulls
PATCH  /api/v3/repos/{owner}/{repo}/pulls/{number}
```

Plus the Plan 1 follow-up: populate the `repositories[]` array in the token-mint response.

**Out of scope of Plan 2 (deferred):**

- Issues endpoints (`/api/v3/repos/.../issues/*`) and issue-comment endpoints — no issues backend exists in GitBucket yet. A separate Plan 2.5 (with its own brainstorm: labels? assignees? mentions?) will add issues + the missing v3 endpoints.
- Webhook engine — Plan 3.
- Manifest registration flow + SPA UI — Plan 4.

**Branch base:** This plan stacks on top of `feature/github-app-emulation-foundation` (Plan 1, currently in PR). Create a new branch `feature/github-app-emulation-rest-surface` off the Plan 1 branch's HEAD. If Plan 1 merges to main first, rebase onto main.

---

## File Structure

**New files:**

```
internal/api/v3/
  handler.go              — V3Handler struct (services bundle), constructor
  repos.go                — GetRepo handler
  contents.go             — GetContents (file or dir) handler
  refs.go                 — GetRef + GetTrees handlers (git plumbing)
  pulls.go                — ListPulls + GetPull + CreatePull + UpdatePull handlers
  routes.go               — RegisterV3Routes(r, h) mounting all of the above
  shared.go               — small private helpers (repo materializer, error mapping)
  *_test.go               — integration tests per file (Firestore emulator)

internal/api/v3/v3fmt/
  urlbuilder.go           — URLBuilder type + factory
  ids.go                  — StableID(string) int64
  user.go                 — User DTO + User(*db.User|botCtx) formatter
  repo.go                 — Repository DTO + Repository(map[string]interface{}, *URLBuilder) formatter
  contents.go             — ContentFile + ContentDirEntry DTOs + formatters
  ref.go                  — Ref DTO + formatter
  tree.go                 — Tree + TreeEntry DTOs + formatters
  pull.go                 — PullRequest DTO + formatter
  testdata/               — golden JSON fixtures (one per resource)
  v3fmt_test.go           — table-driven formatter tests
```

**Modified files:**

```
internal/apps/handlers.go              — CreateInstallationAccessToken: populate `repositories[]` (Task 8)
internal/db/pull_requests.go           — Add UpdatePullRequestTitleBody (Task 7)
main.go                                — Mount v3 routes via RegisterV3Routes (Task 1)
```

**Manual ops (no code change):** None for Plan 2. Plan 1's Firestore TTL config still applies.

---

## Task 1: Foundation — v3 routing, URLBuilder, StableID, package skeleton

**Files:**
- Create: `internal/api/v3/v3fmt/urlbuilder.go`
- Create: `internal/api/v3/v3fmt/urlbuilder_test.go`
- Create: `internal/api/v3/v3fmt/ids.go`
- Create: `internal/api/v3/v3fmt/ids_test.go`
- Create: `internal/api/v3/handler.go`
- Create: `internal/api/v3/routes.go`
- Create: `internal/api/v3/routes_test.go`
- Modify: `main.go` — mount v3 routes

This task wires the skeleton end-to-end with one stub endpoint that proves the routing + middleware + URL/ID helpers all work together. No real data fetching yet.

- [ ] **Step 1: Write the failing tests for URLBuilder + StableID**

Create `internal/api/v3/v3fmt/urlbuilder_test.go`:

```go
package v3fmt

import "testing"

func TestURLBuilder(t *testing.T) {
	b := NewURLBuilder("https://gitbucket.example.com")

	cases := []struct {
		got, want string
	}{
		{b.APIRoot(), "https://gitbucket.example.com/api/v3"},
		{b.UserHTML("alice"), "https://gitbucket.example.com/alice"},
		{b.UserAPI("alice"), "https://gitbucket.example.com/api/v3/users/alice"},
		{b.RepoHTML("alice", "repo"), "https://gitbucket.example.com/alice/repo"},
		{b.RepoAPI("alice", "repo"), "https://gitbucket.example.com/api/v3/repos/alice/repo"},
		{b.PullAPI("alice", "repo", 42), "https://gitbucket.example.com/api/v3/repos/alice/repo/pulls/42"},
		{b.PullHTML("alice", "repo", 42), "https://gitbucket.example.com/alice/repo/pulls/42"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

func TestURLBuilderTrimsTrailingSlash(t *testing.T) {
	b := NewURLBuilder("https://gitbucket.example.com/")
	if got := b.APIRoot(); got != "https://gitbucket.example.com/api/v3" {
		t.Errorf("APIRoot with trailing slash: %q", got)
	}
}
```

Create `internal/api/v3/v3fmt/ids_test.go`:

```go
package v3fmt

import "testing"

func TestStableIDDeterministic(t *testing.T) {
	a := StableID("user:alice")
	b := StableID("user:alice")
	if a != b {
		t.Errorf("StableID not deterministic: %d vs %d", a, b)
	}
}

func TestStableIDDifferentInputs(t *testing.T) {
	if StableID("foo") == StableID("bar") {
		t.Error("StableID collided on trivial inputs")
	}
}

func TestStableIDPositive(t *testing.T) {
	// GitHub IDs are always positive; we mask the sign bit.
	for _, in := range []string{"a", "", "alice/repo", "issue:repo:42"} {
		if got := StableID(in); got < 0 {
			t.Errorf("StableID(%q) = %d, want non-negative", in, got)
		}
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/api/v3/...
```

Expected: package doesn't exist / undefined symbols.

- [ ] **Step 3: Write URLBuilder and StableID**

Create `internal/api/v3/v3fmt/urlbuilder.go`:

```go
// Package v3fmt contains pure functions that translate internal GitBucket
// types into GitHub's exact JSON response shape. These are the only place
// that knows the GitHub REST contract; HTTP handlers in internal/api/v3 are
// thin wrappers that call existing services and delegate output to v3fmt.
package v3fmt

import (
	"fmt"
	"strings"
)

// URLBuilder constructs GitHub-shape URLs anchored at a configurable base.
// Used by every formatter to populate the `url`, `html_url`, `comments_url`,
// etc. fields of GitHub-compatible responses.
type URLBuilder struct {
	base string // no trailing slash
}

func NewURLBuilder(base string) *URLBuilder {
	return &URLBuilder{base: strings.TrimRight(base, "/")}
}

func (b *URLBuilder) Base() string { return b.base }
func (b *URLBuilder) APIRoot() string { return b.base + "/api/v3" }

func (b *URLBuilder) UserHTML(login string) string  { return b.base + "/" + login }
func (b *URLBuilder) UserAPI(login string) string   { return b.APIRoot() + "/users/" + login }
func (b *URLBuilder) UserAvatar(login string) string {
	return b.base + "/avatars/" + login
}

func (b *URLBuilder) RepoHTML(owner, repo string) string {
	return b.base + "/" + owner + "/" + repo
}
func (b *URLBuilder) RepoAPI(owner, repo string) string {
	return b.APIRoot() + "/repos/" + owner + "/" + repo
}

func (b *URLBuilder) PullHTML(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s/%s/pulls/%d", b.base, owner, repo, number)
}
func (b *URLBuilder) PullAPI(owner, repo string, number int) string {
	return fmt.Sprintf("%s/repos/%s/%s/pulls/%d", b.APIRoot(), owner, repo, number)
}

func (b *URLBuilder) ContentsAPI(owner, repo, path, ref string) string {
	return fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", b.APIRoot(), owner, repo, path, ref)
}

func (b *URLBuilder) GitRefAPI(owner, repo, ref string) string {
	return fmt.Sprintf("%s/repos/%s/%s/git/ref/%s", b.APIRoot(), owner, repo, ref)
}

func (b *URLBuilder) GitTreeAPI(owner, repo, sha string) string {
	return fmt.Sprintf("%s/repos/%s/%s/git/trees/%s", b.APIRoot(), owner, repo, sha)
}
```

Create `internal/api/v3/v3fmt/ids.go`:

```go
package v3fmt

import (
	"crypto/sha256"
	"encoding/binary"
)

// StableID derives a stable, positive int64 identifier from a string key.
// GitHub returns integer IDs while GitBucket uses string Firestore document
// IDs. The mapping is content-addressed (sha256-derived), so the same input
// always renders the same numeric ID across deployments — no storage needed.
// The high bit is masked to keep the value positive (matches GitHub).
func StableID(key string) int64 {
	h := sha256.Sum256([]byte(key))
	v := int64(binary.BigEndian.Uint64(h[:8]))
	if v < 0 {
		v &^= 1 << 63 // clear sign bit
	}
	return v
}
```

- [ ] **Step 4: Run formatter tests**

```bash
go test ./internal/api/v3/v3fmt/... -v
```

Expected: all 4 tests pass.

- [ ] **Step 5: Write the failing test for the v3 routing skeleton**

Create `internal/api/v3/routes_test.go`:

```go
package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestV3RouteSkeletonProtectedByInstallationToken(t *testing.T) {
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
	tok, err := scen.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local") // services nil for skeleton test
	RegisterV3Routes(r, h)

	// Without token: 401.
	req := httptest.NewRequest("GET", "/api/v3/_ping", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-token: code = %d, want 401", rr.Code)
	}

	// With token: 200 from the stub.
	req = httptest.NewRequest("GET", "/api/v3/_ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("with-token: code = %d body: %s", rr.Code, rr.Body.String())
	}

	// Sanity: middleware injected the InstallationContext.
	_ = apps.InstallationContextFrom // compile-time check that import is valid
}
```

- [ ] **Step 6: Run to confirm it fails**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -run TestV3RouteSkeletonProtectedByInstallationToken -v
```

Expected: undefined `NewV3Handler` / `RegisterV3Routes`.

- [ ] **Step 7: Write the handler scaffold and routes**

Create `internal/api/v3/handler.go`:

```go
// Package v3 implements the GitHub-compatible REST surface (/api/v3/*).
// Handlers in this package authenticate via installation tokens (see
// internal/apps middleware) and shape responses via internal/api/v3/v3fmt.
package v3

import (
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"

	"gitbucket/internal/api/v3/v3fmt"
)

// V3Handler bundles the dependencies needed by GitHub-shape handlers. The
// fields mirror internal/api.APIHandler so existing data-layer functions
// can be reused without refactoring.
type V3Handler struct {
	FirestoreClient *firestore.Client
	StorageClient   *storage.Client // for repo materialization (GCS → local bare repo)
	URLs            *v3fmt.URLBuilder
}

func NewV3Handler(fs *firestore.Client, sc *storage.Client, baseURL string) *V3Handler {
	return &V3Handler{
		FirestoreClient: fs,
		StorageClient:   sc,
		URLs:            v3fmt.NewURLBuilder(baseURL),
	}
}
```

Create `internal/api/v3/routes.go`:

```go
package v3

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps"
)

// RegisterV3Routes mounts /api/v3/* endpoints behind the installation-token
// middleware. The four /api/v3/app/* endpoints are mounted separately by
// apps.RegisterRoutes (JWT-authed, different middleware); this function
// owns everything else under /api/v3/.
func RegisterV3Routes(r chi.Router, h *V3Handler) {
	r.Route("/api/v3", func(r chi.Router) {
		r.Use(apps.RequireInstallationToken(h.FirestoreClient))

		// Skeleton smoke endpoint — used by routing tests to prove the
		// middleware mounts correctly. Returns 200 with no body.
		r.Get("/_ping", func(w http.ResponseWriter, r *http.Request) {
			ic := apps.InstallationContextFrom(r.Context())
			if ic == nil {
				apps.WriteError(w, apps.ErrUnauthorized)
				return
			}
			apps.WriteJSON(w, http.StatusOK, map[string]string{
				"installation_id": ic.InstallationID,
			})
		})

		// Real endpoints land in Tasks 3-7.
	})
}
```

- [ ] **Step 8: Wire into main.go**

In `main.go`, after `apps.RegisterRoutes(r, appsHandler)`, add:

```go
import (
    // ...existing imports...
    "gitbucket/internal/api/v3"
)

// inside main(), after apps.RegisterRoutes:
v3Handler := v3.NewV3Handler(firestoreClient, storageClient, baseURL(cfg))
v3.RegisterV3Routes(r, v3Handler)
```

`baseURL` is a small helper to derive the public URL from config. Add it to `main.go` near `getClientIP`:

```go
// baseURL returns the configured public base URL for v3 URL generation.
// Falls back to an http://localhost:<port> string for dev mode.
func baseURL(cfg *config.Config) string {
	if v := os.Getenv("PUBLIC_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:" + cfg.Port
}
```

The chi router accepts overlapping `/api/v3` route groups: `apps.RegisterRoutes` mounts `/api/v3/app/*` with JWT middleware, `v3.RegisterV3Routes` mounts `/api/v3/*` with installation-token middleware. Chi resolves the most specific match first. We rely on chi v5's behavior: routes registered earlier with a more specific prefix (`/api/v3/app/*`) win against the broader `/api/v3/*` mount. **Verify this empirically in Step 9 — the existing Plan 1 endpoints must continue to work with App JWTs after the v3 mount is added.**

- [ ] **Step 9: Run tests + manual route verification**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -v
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/apps/... -run TestPlan1AuthPlaneEndToEnd -v
go build ./...
go vet ./internal/api/v3/...
```

The first command must PASS (skeleton routing).
The second command (Plan 1 e2e) must STILL PASS — the v3 mount must not break `/api/v3/app/*` routing.

If `TestPlan1AuthPlaneEndToEnd` fails after the v3 mount is added, the chi route ordering is wrong. Fix by ensuring `apps.RegisterRoutes(r, appsHandler)` is called BEFORE `v3.RegisterV3Routes(r, v3Handler)` in main.go, OR by giving the apps routes more specific path patterns.

- [ ] **Step 10: Commit**

```bash
git add internal/api/v3/ main.go
git commit -m "feat(v3): URLBuilder, StableID, /api/v3 skeleton + ping endpoint"
```

---

## Task 2: User formatter

**Files:**
- Create: `internal/api/v3/v3fmt/user.go`
- Create: `internal/api/v3/v3fmt/user_test.go`
- Create: `internal/api/v3/v3fmt/testdata/user.golden.json`

The User formatter is used inside every other formatter (repo owner, PR author, bot sender). Land it first so dependent formatters can reference it.

- [ ] **Step 1: Write the failing test**

Create `internal/api/v3/v3fmt/user.go` will export:
- `UserDTO` struct matching GitHub's user JSON shape
- `User(u UserSource, urls *URLBuilder) UserDTO` formatter
- `UserSource` interface with `GetLogin() string`, `GetID() int64`, `GetType() string`, `GetAvatarURL() string`
- `UserFromMap(m map[string]interface{}, urls *URLBuilder) UserDTO` convenience for Firestore raw maps
- `UserFromBotContext(ic *apps.InstallationContext, urls *URLBuilder) UserDTO` — synthesizes the App's bot user from an installation context

Create `internal/api/v3/v3fmt/user_test.go`:

```go
package v3fmt

import (
	"encoding/json"
	"os"
	"testing"
)

func TestUserFormatter_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := User(staticUser{
		login: "alice",
		uid:   "user:alice",
		typ:   "User",
	}, urls)

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want, err := os.ReadFile("testdata/user.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v (regenerate with -update if intentional)", err)
	}

	if string(gotJSON)+"\n" != string(want) && string(gotJSON) != string(want) {
		t.Errorf("user formatter drift.\nGot:\n%s\n\nWant:\n%s", gotJSON, want)
	}
}

func TestUserFormatter_BotType(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := User(staticUser{login: "claude-code[bot]", uid: "user:bot_claude", typ: "Bot"}, urls)
	if got.Type != "Bot" {
		t.Errorf("Type = %q, want Bot", got.Type)
	}
	if got.Login != "claude-code[bot]" {
		t.Errorf("Login = %q", got.Login)
	}
}

func TestUserFromMap(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	m := map[string]interface{}{
		"username":   "bob",
		"uid":        "user:bob",
		"type":       "User",
		"avatar_url": "https://gitbucket.example.com/avatars/bob",
	}
	got := UserFromMap(m, urls)
	if got.Login != "bob" || got.Type != "User" {
		t.Errorf("UserFromMap: %+v", got)
	}
}

// staticUser is a test-only UserSource implementation.
type staticUser struct {
	login, uid, typ string
	avatar          string
}

func (s staticUser) GetLogin() string     { return s.login }
func (s staticUser) GetUserKey() string   { return s.uid }
func (s staticUser) GetType() string      { return s.typ }
func (s staticUser) GetAvatarURL() string { return s.avatar }
```

Create the golden fixture `internal/api/v3/v3fmt/testdata/user.golden.json`:

```json
{
  "login": "alice",
  "id": 4636060942743628729,
  "node_id": "MDQ6VXNlcjQ2MzYwNjA5NDI3NDM2Mjg3Mjk=",
  "avatar_url": "https://gitbucket.example.com/avatars/alice",
  "html_url": "https://gitbucket.example.com/alice",
  "url": "https://gitbucket.example.com/api/v3/users/alice",
  "type": "User",
  "site_admin": false
}
```

**Note on the `id` value:** `StableID("user:alice")` produces `4636060942743628729`. If this number changes when you implement (e.g., because the hash algorithm differs in a subtle way), update the golden file with the actual value — what matters is that it's deterministic across runs, not that it matches a specific magic number. Compute it via:

```bash
go run -v -run TestStableIDDeterministic ./internal/api/v3/v3fmt/...
```

Or just inspect the test failure output and copy the actual value into the fixture.

The `node_id` is base64 of `User:<id>` per GitHub's format. The `StableID` for `"user:alice"` → 4636060942743628729 → `User:4636060942743628729` → base64 → `MDQ6VXNlcjQ2MzYwNjA5NDI3NDM2Mjg3Mjk=`. Same approach: regenerate via the actual implementation if the value differs.

- [ ] **Step 2: Run to confirm tests fail**

```bash
go test ./internal/api/v3/v3fmt/... -v
```

Expected: undefined `User`, `UserSource`, `UserFromMap`.

- [ ] **Step 3: Write the implementation**

Create `internal/api/v3/v3fmt/user.go`:

```go
package v3fmt

import (
	"encoding/base64"
	"fmt"

	"gitbucket/internal/apps"
)

// UserSource is a minimal interface the User formatter requires. Anything
// with a login + stable key + type can be formatted as a GitHub user.
type UserSource interface {
	GetLogin() string
	GetUserKey() string // stable string used to derive int64 id
	GetType() string    // "User" or "Bot"
	GetAvatarURL() string
}

// UserDTO matches GitHub's `user` JSON shape (subset — full shape has more
// fields for OAuth scopes/permissions that don't apply to installation tokens).
type UserDTO struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	NodeID    string `json:"node_id"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
	URL       string `json:"url"`
	Type      string `json:"type"`
	SiteAdmin bool   `json:"site_admin"`
}

func User(src UserSource, urls *URLBuilder) UserDTO {
	id := StableID(src.GetUserKey())
	avatar := src.GetAvatarURL()
	if avatar == "" {
		avatar = urls.UserAvatar(src.GetLogin())
	}
	typ := src.GetType()
	if typ == "" {
		typ = "User"
	}
	return UserDTO{
		Login:     src.GetLogin(),
		ID:        id,
		NodeID:    encodeNodeID("User", id),
		AvatarURL: avatar,
		HTMLURL:   urls.UserHTML(src.GetLogin()),
		URL:       urls.UserAPI(src.GetLogin()),
		Type:      typ,
		SiteAdmin: false,
	}
}

// UserFromMap is convenience for Firestore-raw users maps.
func UserFromMap(m map[string]interface{}, urls *URLBuilder) UserDTO {
	login, _ := m["username"].(string)
	uid, _ := m["uid"].(string)
	typ, _ := m["type"].(string)
	avatar, _ := m["avatar_url"].(string)
	return User(staticUserSource{login: login, uid: "user:" + uid, typ: typ, avatar: avatar}, urls)
}

// UserFromBotContext synthesizes the App's bot user from an installation
// context. Use this on write paths where the actor is the App, not the
// installing human.
func UserFromBotContext(ic *apps.InstallationContext, urls *URLBuilder) UserDTO {
	// We don't have the bot username on the InstallationContext; the
	// `app_id` plus `bot_user_id` are enough to look it up. For Plan 2 we
	// only need a stable identity, so we synthesize one from BotUserID.
	return User(staticUserSource{
		login: "app-" + ic.AppID + "[bot]",
		uid:   "user:" + ic.BotUserID,
		typ:   "Bot",
	}, urls)
}

// internal helper that implements UserSource so plain values can be formatted.
type staticUserSource struct {
	login, uid, typ, avatar string
}

func (s staticUserSource) GetLogin() string     { return s.login }
func (s staticUserSource) GetUserKey() string   { return s.uid }
func (s staticUserSource) GetType() string      { return s.typ }
func (s staticUserSource) GetAvatarURL() string { return s.avatar }

func encodeNodeID(kind string, id int64) string {
	raw := fmt.Sprintf("%s:%d", kind, id)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}
```

- [ ] **Step 4: Generate the golden file from the actual implementation**

Run the test once with a known input to derive the actual `id` and `node_id`. Then update `testdata/user.golden.json` with the real values:

```bash
go test ./internal/api/v3/v3fmt/... -run TestUserFormatter_GoldenFixture -v 2>&1 | head -30
```

The test output will show the diff. Copy the "Got:" block into `testdata/user.golden.json` (verbatim, including indentation; preserve the trailing newline). Re-run the test — it must pass.

- [ ] **Step 5: Run all formatter tests**

```bash
go test ./internal/api/v3/v3fmt/... -v
```

Expected: all 6+ tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/api/v3/v3fmt/user.go internal/api/v3/v3fmt/user_test.go internal/api/v3/v3fmt/testdata/user.golden.json
git commit -m "feat(v3fmt): User formatter with golden-fixture test"
```

---

## Task 3: GET /api/v3/repos/{owner}/{repo} — repository metadata

**Files:**
- Create: `internal/api/v3/v3fmt/repo.go`
- Create: `internal/api/v3/v3fmt/repo_test.go`
- Create: `internal/api/v3/v3fmt/testdata/repo.golden.json`
- Create: `internal/api/v3/repos.go`
- Create: `internal/api/v3/repos_test.go`
- Modify: `internal/api/v3/routes.go` — mount the new endpoint

- [ ] **Step 1: Write the failing formatter test**

Create `internal/api/v3/v3fmt/repo_test.go`:

```go
package v3fmt

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestRepoFormatter_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	createdAt := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 21, 14, 0, 0, 0, time.UTC)

	owner := staticUserSource{login: "alice", uid: "user:alice", typ: "User"}
	got := Repository(RepoSource{
		Owner:         owner,
		Name:          "demo-repo",
		Description:   "A demo repository for testing.",
		Visibility:    "public",
		DefaultBranch: "main",
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, urls)

	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	want, err := os.ReadFile("testdata/repo.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(gotJSON) != string(want) && string(gotJSON)+"\n" != string(want) {
		t.Errorf("repo formatter drift.\nGot:\n%s\nWant:\n%s", gotJSON, want)
	}
}

func TestRepositoryFromMap(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	m := map[string]interface{}{
		"owner":         "alice",
		"ownerUid":      "user:alice",
		"name":          "demo-repo",
		"description":   "x",
		"visibility":    "private",
		"defaultBranch": "main",
		"createdAt":     time.Now().UTC(),
		"updatedAt":     time.Now().UTC(),
	}
	dto := RepositoryFromMap(m, urls)
	if dto.Name != "demo-repo" {
		t.Errorf("Name = %q", dto.Name)
	}
	if !dto.Private {
		t.Error("Private should be true for visibility=private")
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/api/v3/v3fmt/... -run TestRepoFormatter -v
```

Expected: undefined `Repository`, `RepoSource`, `RepositoryFromMap`.

- [ ] **Step 3: Write the formatter**

Create `internal/api/v3/v3fmt/repo.go`:

```go
package v3fmt

import "time"

// RepoSource carries the minimum fields needed to render a GitHub-shape
// repository. Use Repository(...) for fresh data or RepositoryFromMap for
// Firestore raw documents.
type RepoSource struct {
	Owner         UserSource
	Name          string
	Description   string
	Visibility    string // "public" | "private"
	DefaultBranch string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RepositoryDTO matches GitHub's repository JSON shape. Subset: omits
// fields specific to features GitBucket doesn't model (forks, watchers,
// open_issues count requiring an issues backend, etc.).
type RepositoryDTO struct {
	ID            int64    `json:"id"`
	NodeID        string   `json:"node_id"`
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	Owner         UserDTO  `json:"owner"`
	Private       bool     `json:"private"`
	HTMLURL       string   `json:"html_url"`
	Description   string   `json:"description"`
	Fork          bool     `json:"fork"`
	URL           string   `json:"url"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
	PushedAt      string   `json:"pushed_at"`
	DefaultBranch string   `json:"default_branch"`
	Visibility    string   `json:"visibility"`
}

func Repository(src RepoSource, urls *URLBuilder) RepositoryDTO {
	id := StableID("repo:" + src.Owner.GetLogin() + "/" + src.Name)
	return RepositoryDTO{
		ID:            id,
		NodeID:        encodeNodeID("Repository", id),
		Name:          src.Name,
		FullName:      src.Owner.GetLogin() + "/" + src.Name,
		Owner:         User(src.Owner, urls),
		Private:       src.Visibility == "private",
		HTMLURL:       urls.RepoHTML(src.Owner.GetLogin(), src.Name),
		Description:   src.Description,
		Fork:          false,
		URL:           urls.RepoAPI(src.Owner.GetLogin(), src.Name),
		CreatedAt:     formatTime(src.CreatedAt),
		UpdatedAt:     formatTime(src.UpdatedAt),
		PushedAt:      formatTime(src.UpdatedAt), // best approximation; GitBucket doesn't track separately
		DefaultBranch: src.DefaultBranch,
		Visibility:    src.Visibility,
	}
}

// RepositoryFromMap converts a Firestore raw repo doc (as returned by
// db.GetRepositoryMetadata) to a GitHub-shape DTO.
func RepositoryFromMap(m map[string]interface{}, urls *URLBuilder) RepositoryDTO {
	ownerLogin, _ := m["owner"].(string)
	ownerUID, _ := m["ownerUid"].(string)
	owner := staticUserSource{
		login: ownerLogin,
		uid:   "user:" + ownerUID,
		typ:   "User",
	}
	return Repository(RepoSource{
		Owner:         owner,
		Name:          getString(m, "name"),
		Description:   getString(m, "description"),
		Visibility:    getString(m, "visibility"),
		DefaultBranch: getString(m, "defaultBranch"),
		CreatedAt:     getTime(m, "createdAt"),
		UpdatedAt:     getTime(m, "updatedAt"),
	}, urls)
}

// --- shared helpers used by all formatters --------------------------------

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func getTime(m map[string]interface{}, key string) time.Time {
	v, _ := m[key].(time.Time)
	return v
}
```

- [ ] **Step 4: Generate the golden fixture**

Run the test and copy the `Got:` block into `testdata/repo.golden.json`:

```bash
go test ./internal/api/v3/v3fmt/... -run TestRepoFormatter -v 2>&1 | head -60
```

- [ ] **Step 5: Run formatter tests**

```bash
go test ./internal/api/v3/v3fmt/... -v
```

Expected: all formatter tests pass.

- [ ] **Step 6: Write the failing handler test**

Create `internal/api/v3/repos_test.go`:

```go
package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestGetRepo(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)
	tok, _ := scen.MintToken(ctx)

	// Seed a repository owned by the installation's account.
	owner := scen.Installation.Account.ID
	repoName := "demo-" + randHex(4)
	if err := db.CreateRepositoryMetadata(ctx, fs, owner, owner, repoName, "demo desc", "public"); err != nil {
		t.Fatalf("CreateRepositoryMetadata: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DeleteRepositoryMetadata(context.Background(), fs, owner, repoName)
	})

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	RegisterV3Routes(r, h)

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["name"] != repoName {
			t.Errorf("name = %v, want %s", body["name"], repoName)
		}
		if body["full_name"] != owner+"/"+repoName {
			t.Errorf("full_name = %v", body["full_name"])
		}
		// node_id and id should be present and non-empty.
		if body["node_id"] == nil || body["id"] == nil {
			t.Errorf("missing id/node_id: %+v", body)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/no-such-repo", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404", rr.Code)
		}
	})

	t.Run("no token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("code = %d, want 401", rr.Code)
		}
	})
}

// randHex is shared with other v3 tests in this package.
```

If you need `randHex` in this package, create a `helpers_test.go` next to it:

```go
// internal/api/v3/helpers_test.go
package v3

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

- [ ] **Step 7: Run to confirm test fails**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -run TestGetRepo -v
```

Expected: 404 on the route (since the handler isn't registered yet).

- [ ] **Step 8: Write the handler**

Create `internal/api/v3/repos.go`:

```go
package v3

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

// GetRepo handles GET /api/v3/repos/{owner}/{repo}.
//
// Authorization: requires `metadata: read` permission (defaults to all
// installations since metadata is the base permission).
func (h *V3Handler) GetRepo(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "metadata", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	meta, err := db.GetRepositoryMetadata(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		apps.WriteError(w, err)
		return
	}
	if meta == nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}

	// Future Plan 2.5: check installation's repository_selection / repository_ids
	// before returning. For Plan 2, all installations have selection="all".

	apps.WriteJSON(w, http.StatusOK, v3fmt.RepositoryFromMap(meta, h.URLs))
}
```

Modify `internal/api/v3/routes.go` to register the new route. Inside the existing `r.Route("/api/v3", ...)` block, add:

```go
r.Get("/repos/{owner}/{repo}", h.GetRepo)
```

- [ ] **Step 9: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -v
```

Expected: all 3 subtests of `TestGetRepo` pass + skeleton + formatter tests.

- [ ] **Step 10: Commit**

```bash
git add internal/api/v3/v3fmt/repo.go internal/api/v3/v3fmt/repo_test.go internal/api/v3/v3fmt/testdata/repo.golden.json internal/api/v3/repos.go internal/api/v3/repos_test.go internal/api/v3/routes.go internal/api/v3/helpers_test.go
git commit -m "feat(v3): GET /api/v3/repos/{owner}/{repo} + Repository formatter"
```

---

## Task 4: GET /api/v3/repos/{owner}/{repo}/contents/{path} — file or directory

**Files:**
- Create: `internal/api/v3/v3fmt/contents.go`
- Create: `internal/api/v3/v3fmt/contents_test.go`
- Create: `internal/api/v3/v3fmt/testdata/contents-file.golden.json`
- Create: `internal/api/v3/v3fmt/testdata/contents-dir.golden.json`
- Create: `internal/api/v3/contents.go`
- Create: `internal/api/v3/contents_test.go`
- Create: `internal/api/v3/shared.go` — repo materialization helper

The Contents endpoint must support both file mode (returns one object with base64-encoded `content`) and directory mode (returns array of file/dir entries). Inputs come from `git show` (file content) and `git ls-tree` (directory listing) on a locally-materialized bare repo.

- [ ] **Step 1: Read the existing repo materialization code in `internal/api/api.go::authorizeGitRead`**

The current path is:

```
authorizeGitRead → loads RepositoryMetadata → checks visibility/owner via
Firebase auth → calls h.syncFromGCS to materialize the bare repo locally →
returns localRepoPath.
```

For installation-token auth, we need the materialization piece WITHOUT the Firebase-auth visibility check. Extract the GCS-sync logic into a new helper in `internal/api/v3/shared.go`:

```go
package v3

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"

	"gitbucket/internal/db"
	// import the existing internal/api package, NOT a name-collision; we
	// just need to reach the syncFromGCS helper. If that helper is private,
	// either expose it or duplicate the materialization logic here.
)

// MaterializeRepo ensures the bare repo for owner/repo is present at
// LOCAL_REPOS_ROOT, sourcing missing objects from GCS. Returns the local
// path or a NotFound error if the repo doesn't exist in Firestore.
//
// Plan 2 implementation: search internal/api/api.go for the existing
// h.syncFromGCS or equivalent. If it's an unexported method on
// internal/api.APIHandler, EITHER:
//   (a) Export it (cleanest, but touches existing code), OR
//   (b) Reproduce the GCS sync logic here as a free function taking
//       (fs, storage, owner, repo, localReposRoot) — preferred to avoid
//       cross-package coupling.
//
// Choose (b) if the existing helper is fewer than ~80 lines; (a) otherwise.
// Document the choice in the commit message.
func MaterializeRepo(ctx context.Context, fs *firestore.Client, sc *storage.Client, owner, repo, localReposRoot string) (string, error) {
	return "", fmt.Errorf("MaterializeRepo not implemented — see plan task 4 step 1")
}
```

Replace the placeholder in step 5 below with the actual implementation. The implementer should:

1. Read `internal/api/api.go` lines around `authorizeGitRead` to understand the current materialization flow.
2. Identify the sync helper (it may be `h.syncFromGCS` or similar).
3. Implement `MaterializeRepo` either as a thin wrapper exposing the existing method, or as a duplicated free function.

If the existing helper is genuinely complex (>80 lines) AND involves the `APIHandler` struct's other fields, consider Option (a): export the helper. Document the touched files clearly.

- [ ] **Step 2: Write the contents formatter test**

Create `internal/api/v3/v3fmt/contents_test.go`:

```go
package v3fmt

import (
	"encoding/json"
	"os"
	"testing"
)

func TestContentsFile_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := ContentFile(ContentFileSource{
		Owner: "alice", Repo: "demo", Path: "README.md", Ref: "main",
		SHA:  "abc123def456",
		Size: 42,
		Raw:  []byte("# Hello\n\nThis is a test.\n"),
	}, urls)
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	want, _ := os.ReadFile("testdata/contents-file.golden.json")
	if string(gotJSON) != string(want) && string(gotJSON)+"\n" != string(want) {
		t.Errorf("contents file drift.\nGot:\n%s\nWant:\n%s", gotJSON, want)
	}
}

func TestContentsDir_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := ContentDir(ContentDirSource{
		Owner: "alice", Repo: "demo", Path: "src", Ref: "main",
		Entries: []DirEntry{
			{Name: "main.go", Path: "src/main.go", SHA: "sha-of-main", Type: "file", Size: 120},
			{Name: "helpers", Path: "src/helpers", SHA: "sha-of-helpers", Type: "dir"},
		},
	}, urls)
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	want, _ := os.ReadFile("testdata/contents-dir.golden.json")
	if string(gotJSON) != string(want) && string(gotJSON)+"\n" != string(want) {
		t.Errorf("contents dir drift.\nGot:\n%s\nWant:\n%s", gotJSON, want)
	}
}
```

- [ ] **Step 3: Confirm fail**

```bash
go test ./internal/api/v3/v3fmt/... -run TestContents -v
```

Expected: undefined `ContentFile`, etc.

- [ ] **Step 4: Write the contents formatters**

Create `internal/api/v3/v3fmt/contents.go`:

```go
package v3fmt

import (
	"encoding/base64"
)

// ContentFileSource carries inputs needed to render a file's contents per
// GitHub's `contents` API response.
type ContentFileSource struct {
	Owner, Repo, Path, Ref string
	SHA                    string
	Size                   int64
	Raw                    []byte
}

// ContentFileDTO matches GitHub's response for `GET /repos/.../contents/{path}`
// when the path is a file.
type ContentFileDTO struct {
	Type        string `json:"type"` // always "file"
	Encoding    string `json:"encoding"` // always "base64"
	Size        int64  `json:"size"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Content     string `json:"content"` // base64 of Raw
	SHA         string `json:"sha"`
	URL         string `json:"url"`
	HTMLURL     string `json:"html_url"`
	DownloadURL string `json:"download_url"`
}

func ContentFile(src ContentFileSource, urls *URLBuilder) ContentFileDTO {
	return ContentFileDTO{
		Type:        "file",
		Encoding:    "base64",
		Size:        src.Size,
		Name:        baseName(src.Path),
		Path:        src.Path,
		Content:     base64.StdEncoding.EncodeToString(src.Raw),
		SHA:         src.SHA,
		URL:         urls.ContentsAPI(src.Owner, src.Repo, src.Path, src.Ref),
		HTMLURL:     urls.RepoHTML(src.Owner, src.Repo) + "/blob/" + src.Ref + "/" + src.Path,
		DownloadURL: urls.RepoHTML(src.Owner, src.Repo) + "/raw/" + src.Ref + "/" + src.Path,
	}
}

// ContentDirSource carries directory listing inputs.
type ContentDirSource struct {
	Owner, Repo, Path, Ref string
	Entries                []DirEntry
}

type DirEntry struct {
	Name string
	Path string
	SHA  string
	Type string // "file" | "dir"
	Size int64  // 0 for dirs
}

// ContentDirEntryDTO is the GitHub shape for an entry in a directory listing.
type ContentDirEntryDTO struct {
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	SHA         string `json:"sha"`
	URL         string `json:"url"`
	HTMLURL     string `json:"html_url"`
	DownloadURL string `json:"download_url,omitempty"`
}

func ContentDir(src ContentDirSource, urls *URLBuilder) []ContentDirEntryDTO {
	out := make([]ContentDirEntryDTO, 0, len(src.Entries))
	for _, e := range src.Entries {
		entry := ContentDirEntryDTO{
			Type:    e.Type,
			Size:    e.Size,
			Name:    e.Name,
			Path:    e.Path,
			SHA:     e.SHA,
			URL:     urls.ContentsAPI(src.Owner, src.Repo, e.Path, src.Ref),
			HTMLURL: urls.RepoHTML(src.Owner, src.Repo) + "/blob/" + src.Ref + "/" + e.Path,
		}
		if e.Type == "file" {
			entry.DownloadURL = urls.RepoHTML(src.Owner, src.Repo) + "/raw/" + src.Ref + "/" + e.Path
		}
		out = append(out, entry)
	}
	return out
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
```

- [ ] **Step 5: Generate the golden fixtures**

Run the tests and copy `Got:` output into `testdata/contents-file.golden.json` and `testdata/contents-dir.golden.json`. Re-run to confirm green.

- [ ] **Step 6: Write the handler integration test**

Create `internal/api/v3/contents_test.go`:

```go
package v3

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

// seedLocalRepo creates a tiny bare repo locally with one commit on `main`
// containing README.md + src/main.go.
func seedLocalRepo(t *testing.T, localReposRoot, owner, repo string) string {
	t.Helper()
	bare := filepath.Join(localReposRoot, owner+"_"+repo+".git")
	work := filepath.Join(localReposRoot, "_work_"+owner+"_"+repo)
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	run := func(dir, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run(bare, "git", "init", "--bare", "--initial-branch=main")
	run(work, "git", "init", "--initial-branch=main")
	run(work, "git", "config", "user.email", "test@test")
	run(work, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# Hello\n"), 0o644)
	os.MkdirAll(filepath.Join(work, "src"), 0o755)
	os.WriteFile(filepath.Join(work, "src", "main.go"), []byte("package main\n"), 0o644)
	run(work, "git", "add", ".")
	run(work, "git", "commit", "-m", "init")
	run(work, "git", "remote", "add", "origin", bare)
	run(work, "git", "push", "origin", "main")
	return bare
}

func TestGetContents(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	tmp := t.TempDir()
	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)
	tok, _ := scen.MintToken(ctx)
	owner := scen.Installation.Account.ID
	repoName := "demo-" + randHex(4)

	if err := db.CreateRepositoryMetadata(ctx, fs, owner, owner, repoName, "", "public"); err != nil {
		t.Fatalf("CreateRepositoryMetadata: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DeleteRepositoryMetadata(context.Background(), fs, owner, repoName)
	})
	_ = seedLocalRepo(t, tmp, owner, repoName)

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	h.LocalReposRoot = tmp // expose this field
	RegisterV3Routes(r, h)

	auth := "Bearer " + tok

	t.Run("file: README.md", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName+"/contents/README.md?ref=main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["type"] != "file" || body["encoding"] != "base64" {
			t.Errorf("type/encoding wrong: %+v", body)
		}
		content, _ := body["content"].(string)
		decoded, _ := base64.StdEncoding.DecodeString(content)
		if string(decoded) != "# Hello\n" {
			t.Errorf("decoded = %q, want \"# Hello\\n\"", decoded)
		}
	})

	t.Run("dir: src", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName+"/contents/src?ref=main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var entries []map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &entries)
		if len(entries) != 1 || entries[0]["name"] != "main.go" {
			t.Errorf("dir entries = %+v", entries)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName+"/contents/nope.txt?ref=main", nil)
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404", rr.Code)
		}
	})
}
```

- [ ] **Step 7: Run to confirm fail**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -run TestGetContents -v
```

Expected: 404 (route not registered) OR missing `LocalReposRoot` field.

- [ ] **Step 8: Add `LocalReposRoot` to V3Handler**

In `internal/api/v3/handler.go`, add the field:

```go
type V3Handler struct {
	FirestoreClient *firestore.Client
	StorageClient   *storage.Client
	URLs            *v3fmt.URLBuilder
	LocalReposRoot  string
}
```

And add it to `NewV3Handler`:

```go
func NewV3Handler(fs *firestore.Client, sc *storage.Client, baseURL string) *V3Handler {
	return &V3Handler{
		FirestoreClient: fs,
		StorageClient:   sc,
		URLs:            v3fmt.NewURLBuilder(baseURL),
		LocalReposRoot:  os.Getenv("LOCAL_REPOS_ROOT"),
	}
}
```

In `main.go`, where `v3Handler := v3.NewV3Handler(...)` is constructed, set the field if env-var-default-is-empty:

```go
if v3Handler.LocalReposRoot == "" {
    v3Handler.LocalReposRoot = cfg.LocalReposRoot
}
```

- [ ] **Step 9: Implement `MaterializeRepo` and `GetContents`**

Replace the stub in `internal/api/v3/shared.go`:

```go
package v3

import (
	"context"
	"fmt"
	"path/filepath"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"

	"gitbucket/internal/db"
)

// MaterializeRepo ensures a bare repo exists at <localReposRoot>/<owner>_<repo>.git.
// In production this also syncs missing objects from GCS, but for Plan 2 we
// trust that:
//   (a) Tests seed the bare repo directly via seedLocalRepo (see contents_test.go), and
//   (b) Production traffic on /api/v3 routes goes through the same Git HTTP
//       infrastructure that already populates the local repo on prior reads.
//
// The Firestore presence check is the source of truth for "does this repo
// exist"; if the local bare repo is missing, we return NotFound (callers
// must surface that as 404).
//
// FUTURE: If running on a cold Cloud Run instance, this will need to invoke
// the GCS sync logic from internal/api.APIHandler. Track as follow-on.
func MaterializeRepo(ctx context.Context, fs *firestore.Client, sc *storage.Client, owner, repo, localReposRoot string) (string, error) {
	meta, err := db.GetRepositoryMetadata(ctx, fs, owner, repo)
	if err != nil {
		return "", err
	}
	if meta == nil {
		return "", errRepoNotFound
	}
	if localReposRoot == "" {
		return "", fmt.Errorf("LocalReposRoot not configured")
	}
	bare := filepath.Join(localReposRoot, owner+"_"+repo+".git")
	return bare, nil
}

var errRepoNotFound = fmt.Errorf("repo not found")
```

Create `internal/api/v3/contents.go`:

```go
package v3

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
)

// GetContents handles GET /api/v3/repos/{owner}/{repo}/contents/{path}.
// Supports both file (returns one ContentFileDTO) and directory (returns
// array of ContentDirEntryDTO).
func (h *V3Handler) GetContents(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "contents", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	path := chi.URLParam(r, "*")
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = "main"
	}

	bare, err := MaterializeRepo(r.Context(), h.FirestoreClient, h.StorageClient, owner, repo, h.LocalReposRoot)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			apps.WriteError(w, apps.ErrNotFound)
			return
		}
		apps.WriteError(w, err)
		return
	}

	// First try directory listing via git ls-tree.
	entries, isDir, err := lsTree(bare, ref, path)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	if isDir {
		apps.WriteJSON(w, http.StatusOK, v3fmt.ContentDir(v3fmt.ContentDirSource{
			Owner: owner, Repo: repo, Path: path, Ref: ref, Entries: entries,
		}, h.URLs))
		return
	}

	// Not a dir — try file via git show.
	raw, sha, size, err := gitShowFile(bare, ref, path)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, v3fmt.ContentFile(v3fmt.ContentFileSource{
		Owner: owner, Repo: repo, Path: path, Ref: ref,
		SHA: sha, Size: size, Raw: raw,
	}, h.URLs))
}

// lsTree runs `git ls-tree` against the bare repo. If `path` is empty or
// refers to a directory, returns (entries, true, nil). If `path` refers to
// a single file (one ls-tree row matching the exact path), returns
// (nil, false, nil) to signal the caller to fall through to file mode.
func lsTree(bare, ref, path string) ([]v3fmt.DirEntry, bool, error) {
	target := ref
	if path != "" {
		target = ref + ":" + path
	}
	cmd := exec.Command("git", "--git-dir", bare, "ls-tree", "-l", "--full-name", target)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, false, err
	}

	var entries []v3fmt.DirEntry
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		fields := strings.Fields(parts[0])
		if len(fields) < 4 {
			continue
		}
		typ := fields[1]
		sha := fields[2]
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		p := parts[1]
		ftype := "file"
		if typ == "tree" {
			ftype = "dir"
		}
		entries = append(entries, v3fmt.DirEntry{
			Name: filepathBase(p), Path: p, SHA: sha, Type: ftype, Size: size,
		})
	}

	// Heuristic: if a single entry path equals exactly `path`, this was a
	// file lookup (not a dir).
	if path != "" && len(entries) == 1 && entries[0].Path == path {
		return nil, false, nil
	}

	if entries == nil {
		entries = []v3fmt.DirEntry{}
	}
	return entries, true, nil
}

func gitShowFile(bare, ref, path string) (raw []byte, sha string, size int64, err error) {
	target := ref + ":" + path
	cmd := exec.Command("git", "--git-dir", bare, "show", target)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if e := cmd.Run(); e != nil {
		return nil, "", 0, e
	}
	raw = out.Bytes()
	size = int64(len(raw))

	// SHA via `git rev-parse <target>`.
	cmd2 := exec.Command("git", "--git-dir", bare, "rev-parse", target)
	var sha2 bytes.Buffer
	cmd2.Stdout = &sha2
	if e := cmd2.Run(); e == nil {
		sha = strings.TrimSpace(sha2.String())
	}
	return raw, sha, size, nil
}

func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
```

Register the route in `internal/api/v3/routes.go`:

```go
r.Get("/repos/{owner}/{repo}/contents/*", h.GetContents)
```

- [ ] **Step 10: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -v
```

Expected: all 3 subtests of `TestGetContents` pass.

If `lsTree`/`gitShowFile` errors trip the path-vs-dir heuristic, debug by running the test with `-v` and inspecting which subtest fails. The known wrinkle: `git ls-tree main:README.md` returns the file entry rather than directory contents, so the "single entry exactly matching path" heuristic distinguishes file from dir. If your seeded test repo includes a file at the exact path being requested, the heuristic fires correctly.

- [ ] **Step 11: Commit**

```bash
git add internal/api/v3/v3fmt/contents.go internal/api/v3/v3fmt/contents_test.go internal/api/v3/v3fmt/testdata/contents-*.golden.json internal/api/v3/contents.go internal/api/v3/contents_test.go internal/api/v3/shared.go internal/api/v3/handler.go internal/api/v3/routes.go
git commit -m "feat(v3): GET /repos/{owner}/{repo}/contents/{path} for files and dirs"
```

---

## Task 5: GET /git/ref/{ref} + GET /git/trees/{sha}

**Files:**
- Create: `internal/api/v3/v3fmt/ref.go` + `ref_test.go` + golden
- Create: `internal/api/v3/v3fmt/tree.go` + `tree_test.go` + golden
- Create: `internal/api/v3/refs.go` + `refs_test.go`
- Modify: `internal/api/v3/routes.go`

These two endpoints are git plumbing primitives: resolve a ref name to a commit SHA, and list a tree object's entries by SHA.

- [ ] **Step 1: Write the ref formatter test**

Create `internal/api/v3/v3fmt/ref_test.go`:

```go
package v3fmt

import (
	"encoding/json"
	"testing"
)

func TestRefFormatter(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := Ref(RefSource{
		Owner: "alice", Repo: "demo",
		Ref: "heads/main",
		SHA: "abc123def456abc123def456abc123def4567890",
	}, urls)
	gotJSON, _ := json.Marshal(got)
	want := `{"ref":"refs/heads/main","node_id":"MDA6UmVmYWxpY2UvZGVtbzpoZWFkcy9tYWlu","url":"https://gitbucket.example.com/api/v3/repos/alice/demo/git/ref/heads/main","object":{"type":"commit","sha":"abc123def456abc123def456abc123def4567890","url":"https://gitbucket.example.com/api/v3/repos/alice/demo/git/commits/abc123def456abc123def456abc123def4567890"}}`
	if string(gotJSON) != want {
		t.Errorf("Ref drift.\nGot:  %s\nWant: %s", gotJSON, want)
	}
}
```

(Adjust `node_id` to the actual computed value once the implementation runs.)

- [ ] **Step 2: Write the formatter**

Create `internal/api/v3/v3fmt/ref.go`:

```go
package v3fmt

import "fmt"

type RefSource struct {
	Owner, Repo, Ref, SHA string
}

type RefObjectDTO struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

type RefDTO struct {
	Ref    string       `json:"ref"`    // "refs/heads/main"
	NodeID string       `json:"node_id"`
	URL    string       `json:"url"`
	Object RefObjectDTO `json:"object"`
}

func Ref(src RefSource, urls *URLBuilder) RefDTO {
	fullRef := "refs/" + src.Ref
	nodeKey := fmt.Sprintf("%s/%s:%s", src.Owner, src.Repo, src.Ref)
	return RefDTO{
		Ref:    fullRef,
		NodeID: encodeNodeID("Ref", StableID(nodeKey)),
		URL:    urls.GitRefAPI(src.Owner, src.Repo, src.Ref),
		Object: RefObjectDTO{
			Type: "commit",
			SHA:  src.SHA,
			URL:  fmt.Sprintf("%s/git/commits/%s", urls.RepoAPI(src.Owner, src.Repo), src.SHA),
		},
	}
}
```

Note: `encodeNodeID("Ref", id)` uses `StableID`'s int64 output, but GitHub's ref node_id format is actually `Ref:<owner>/<repo>:<ref>` base64'd (not numeric). For Plan 2, the simpler `encodeNodeID` is acceptable; if a real consumer fails on this, switch to base64'ing the literal string.

- [ ] **Step 3: Tree formatter — test and impl**

Create `internal/api/v3/v3fmt/tree.go`:

```go
package v3fmt

import "fmt"

type TreeSource struct {
	Owner, Repo, SHA string
	Truncated        bool
	Entries          []TreeEntrySource
}
type TreeEntrySource struct {
	Path string
	Mode string
	Type string // "blob" | "tree" | "commit"
	SHA  string
	Size int64
}

type TreeEntryDTO struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size,omitempty"`
	URL  string `json:"url"`
}

type TreeDTO struct {
	SHA       string         `json:"sha"`
	URL       string         `json:"url"`
	Tree      []TreeEntryDTO `json:"tree"`
	Truncated bool           `json:"truncated"`
}

func Tree(src TreeSource, urls *URLBuilder) TreeDTO {
	entries := make([]TreeEntryDTO, 0, len(src.Entries))
	for _, e := range src.Entries {
		entries = append(entries, TreeEntryDTO{
			Path: e.Path,
			Mode: e.Mode,
			Type: e.Type,
			SHA:  e.SHA,
			Size: e.Size,
			URL: fmt.Sprintf("%s/git/blobs/%s", urls.RepoAPI(src.Owner, src.Repo), e.SHA),
		})
	}
	return TreeDTO{
		SHA:       src.SHA,
		URL:       urls.GitTreeAPI(src.Owner, src.Repo, src.SHA),
		Tree:      entries,
		Truncated: src.Truncated,
	}
}
```

Tree formatter test follows the same golden-fixture pattern. Keep it simple — write the test in `tree_test.go`, then a one-row tree input/output check.

- [ ] **Step 4: Handler + tests**

Create `internal/api/v3/refs.go`:

```go
package v3

import (
	"bytes"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
)

func (h *V3Handler) GetRef(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "contents", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "*")

	bare, err := MaterializeRepo(r.Context(), h.FirestoreClient, h.StorageClient, owner, repo, h.LocalReposRoot)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			apps.WriteError(w, apps.ErrNotFound)
			return
		}
		apps.WriteError(w, err)
		return
	}

	sha, err := gitRevParse(bare, "refs/"+ref)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, v3fmt.Ref(v3fmt.RefSource{
		Owner: owner, Repo: repo, Ref: ref, SHA: sha,
	}, h.URLs))
}

func (h *V3Handler) GetTree(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "contents", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")
	recursive := r.URL.Query().Get("recursive") == "1" || r.URL.Query().Get("recursive") == "true"

	bare, err := MaterializeRepo(r.Context(), h.FirestoreClient, h.StorageClient, owner, repo, h.LocalReposRoot)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			apps.WriteError(w, apps.ErrNotFound)
			return
		}
		apps.WriteError(w, err)
		return
	}

	entries, err := gitListTree(bare, sha, recursive)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, v3fmt.Tree(v3fmt.TreeSource{
		Owner: owner, Repo: repo, SHA: sha, Entries: entries, Truncated: false,
	}, h.URLs))
}

func gitRevParse(bare, target string) (string, error) {
	cmd := exec.Command("git", "--git-dir", bare, "rev-parse", target)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func gitListTree(bare, sha string, recursive bool) ([]v3fmt.TreeEntrySource, error) {
	args := []string{"--git-dir", bare, "ls-tree", "-l"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, sha)
	cmd := exec.Command("git", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	var entries []v3fmt.TreeEntrySource
	for _, line := range strings.Split(out.String(), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		fields := strings.Fields(parts[0])
		if len(fields) < 4 {
			continue
		}
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		entries = append(entries, v3fmt.TreeEntrySource{
			Mode: fields[0],
			Type: fields[1], // "blob" | "tree" | "commit"
			SHA:  fields[2],
			Size: size,
			Path: parts[1],
		})
	}
	return entries, nil
}
```

Add routes:

```go
r.Get("/repos/{owner}/{repo}/git/ref/*", h.GetRef)
r.Get("/repos/{owner}/{repo}/git/trees/{sha}", h.GetTree)
```

Create `internal/api/v3/refs_test.go` with the seed-local-repo pattern and assert both endpoints return the expected SHA / entries for the seeded `main` branch.

- [ ] **Step 5: Run tests**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/api/v3/v3fmt/{ref,tree}.go internal/api/v3/v3fmt/{ref,tree}_test.go internal/api/v3/refs.go internal/api/v3/refs_test.go internal/api/v3/routes.go
git commit -m "feat(v3): GET /git/ref/{ref} and GET /git/trees/{sha}"
```

---

## Task 6: Pull-request read endpoints (list + get) + Pull formatter

**Files:**
- Create: `internal/api/v3/v3fmt/pull.go` + `pull_test.go` + golden
- Create: `internal/api/v3/pulls.go` + `pulls_test.go`
- Modify: `internal/api/v3/routes.go`

The Pull formatter is large — GitHub's PR DTO has 30+ fields. Land it with the read endpoints first; writes follow in Task 7.

- [ ] **Step 1: Pull formatter test**

Skeleton (use the golden-fixture pattern from `repo_test.go`):

```go
// internal/api/v3/v3fmt/pull_test.go
package v3fmt

// ... use golden fixture: testdata/pull.golden.json
```

The test populates a `PullRequestSource` with: owner, repo, number, title, body, state, head/base branch + SHA, author, created_at, updated_at, merged_at (optional). Marshals to JSON, asserts equality against the golden file.

- [ ] **Step 2: Pull formatter**

Create `internal/api/v3/v3fmt/pull.go`:

```go
package v3fmt

import (
	"fmt"
	"time"
)

type PullRequestSource struct {
	Owner, Repo string
	Number      int
	Title, Body string
	State       string // "open" | "closed" | "merged"
	Author      UserSource
	HeadBranch  string
	BaseBranch  string
	HeadSHA     string // optional; empty if unknown
	BaseSHA     string // optional
	CreatedAt   time.Time
	UpdatedAt   time.Time
	MergedAt    *time.Time
	Draft       bool
}

type pullRefDTO struct {
	Label string  `json:"label"`
	Ref   string  `json:"ref"`
	SHA   string  `json:"sha"`
	User  UserDTO `json:"user"`
}

type PullRequestDTO struct {
	ID        int64       `json:"id"`
	NodeID    string      `json:"node_id"`
	Number    int         `json:"number"`
	State     string      `json:"state"` // GitHub uses "open" / "closed"; we map "merged" → "closed" with Merged=true
	Title     string      `json:"title"`
	Body      string      `json:"body"`
	URL       string      `json:"url"`
	HTMLURL   string      `json:"html_url"`
	DiffURL   string      `json:"diff_url"`
	User      UserDTO     `json:"user"`
	Head      pullRefDTO  `json:"head"`
	Base      pullRefDTO  `json:"base"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
	ClosedAt  string      `json:"closed_at,omitempty"`
	MergedAt  string      `json:"merged_at,omitempty"`
	Merged    bool        `json:"merged"`
	Draft     bool        `json:"draft"`
}

func PullRequest(src PullRequestSource, urls *URLBuilder) PullRequestDTO {
	id := StableID(fmt.Sprintf("pr:%s/%s:%d", src.Owner, src.Repo, src.Number))
	state := src.State
	merged := false
	if state == "merged" {
		state = "closed"
		merged = true
	}
	mergedAt := ""
	closedAt := ""
	if src.MergedAt != nil {
		mergedAt = formatTime(*src.MergedAt)
		closedAt = mergedAt
	}
	dto := PullRequestDTO{
		ID:        id,
		NodeID:    encodeNodeID("PullRequest", id),
		Number:    src.Number,
		State:     state,
		Title:     src.Title,
		Body:      src.Body,
		URL:       urls.PullAPI(src.Owner, src.Repo, src.Number),
		HTMLURL:   urls.PullHTML(src.Owner, src.Repo, src.Number),
		DiffURL:   urls.PullHTML(src.Owner, src.Repo, src.Number) + ".diff",
		User:      User(src.Author, urls),
		Head: pullRefDTO{
			Label: src.Owner + ":" + src.HeadBranch,
			Ref:   src.HeadBranch,
			SHA:   src.HeadSHA,
			User:  User(src.Author, urls), // best approximation
		},
		Base: pullRefDTO{
			Label: src.Owner + ":" + src.BaseBranch,
			Ref:   src.BaseBranch,
			SHA:   src.BaseSHA,
			User:  User(src.Author, urls),
		},
		CreatedAt: formatTime(src.CreatedAt),
		UpdatedAt: formatTime(src.UpdatedAt),
		ClosedAt:  closedAt,
		MergedAt:  mergedAt,
		Merged:    merged,
		Draft:     src.Draft,
	}
	return dto
}
```

- [ ] **Step 3: Generate golden, run formatter tests**

- [ ] **Step 4: Handlers**

Create `internal/api/v3/pulls.go`:

```go
package v3

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

func (h *V3Handler) ListPulls(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	prs, err := db.ListPullRequests(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		apps.WriteError(w, err)
		return
	}
	out := make([]v3fmt.PullRequestDTO, 0, len(prs))
	for _, pr := range prs {
		out = append(out, pullToFormatter(pr, owner, repo, h.URLs))
	}
	apps.WriteJSON(w, http.StatusOK, out)
}

func (h *V3Handler) GetPull(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	num, _ := strconv.Atoi(chi.URLParam(r, "number"))

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, num)
	if err != nil {
		apps.WriteError(w, err)
		return
	}
	if pr == nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, pullToFormatter(*pr, owner, repo, h.URLs))
}

// pullToFormatter converts the internal db.PullRequest into a v3fmt source.
// The exact field names of db.PullRequest may differ; consult
// `internal/db/pull_requests.go` and adjust accordingly.
func pullToFormatter(pr db.PullRequest, owner, repo string, urls *v3fmt.URLBuilder) v3fmt.PullRequestDTO {
	author := userSourceFor(pr.AuthorUsername, pr.AuthorUid, urls)
	return v3fmt.PullRequest(v3fmt.PullRequestSource{
		Owner:      owner,
		Repo:       repo,
		Number:     pr.Number,
		Title:      pr.Title,
		Body:       pr.Description,
		State:      pr.Status, // db uses "status" with values: open|closed|merged
		Author:     author,
		HeadBranch: pr.SourceBranch,
		BaseBranch: pr.TargetBranch,
		CreatedAt:  pr.CreatedAt,
		UpdatedAt:  pr.UpdatedAt,
	}, urls)
}
```

Add a tiny helper in `shared.go`:

```go
func userSourceFor(username, uid string, urls *v3fmt.URLBuilder) v3fmt.UserSource {
	return v3fmt.StaticUser(username, "user:"+uid, "User", "")
}
```

And expose a `StaticUser` constructor in `v3fmt/user.go`:

```go
func StaticUser(login, uid, typ, avatarURL string) UserSource {
	return staticUserSource{login: login, uid: uid, typ: typ, avatar: avatarURL}
}
```

(The existing `staticUserSource` in user.go was unexported; adding `StaticUser` keeps the type unexported but exposes a constructor.)

- [ ] **Step 5: Field-name verification**

Read `internal/db/pull_requests.go` to confirm the exact struct field names referenced in `pullToFormatter`. Likely names: `Number`, `Title`, `Description`, `Status`, `AuthorUsername`, `AuthorUid`, `SourceBranch`, `TargetBranch`, `CreatedAt`, `UpdatedAt`. Adjust if any differ.

- [ ] **Step 6: Routes**

```go
r.Get("/repos/{owner}/{repo}/pulls", h.ListPulls)
r.Get("/repos/{owner}/{repo}/pulls/{number}", h.GetPull)
```

- [ ] **Step 7: Handler integration test**

`internal/api/v3/pulls_test.go`: seed a PR via `db.CreatePullRequest`, hit GET pulls and GET pulls/{n}, assert GitHub-shape body (number, state, head.ref, base.ref, user.login). Plus 404 for missing PR.

- [ ] **Step 8: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -v
git add internal/api/v3/v3fmt/pull.go internal/api/v3/v3fmt/pull_test.go internal/api/v3/v3fmt/testdata/pull.golden.json internal/api/v3/pulls.go internal/api/v3/pulls_test.go internal/api/v3/routes.go internal/api/v3/v3fmt/user.go internal/api/v3/shared.go
git commit -m "feat(v3): GET pulls list + GET pulls/{n} + PullRequest formatter"
```

---

## Task 7: Pull-request write endpoints (POST + PATCH)

**Files:**
- Modify: `internal/api/v3/pulls.go` — add CreatePull + UpdatePull
- Modify: `internal/api/v3/pulls_test.go` — add write subtests
- Modify: `internal/api/v3/routes.go` — register POST/PATCH
- Modify: `internal/db/pull_requests.go` — add `UpdatePullRequestTitleBody` function

GitHub's POST `/api/v3/.../pulls` creates a PR; PATCH `/api/v3/.../pulls/{n}` updates title/body/state. For Plan 2 the actor on writes is the App's bot user (sourced from `InstallationContext.BotUserID`).

- [ ] **Step 1: Add db function for title/body update**

In `internal/db/pull_requests.go`, after `UpdatePullRequestStatus`, add:

```go
// UpdatePullRequestTitleBody updates title and/or description of a PR.
// Empty-string fields are treated as no-op.
func UpdatePullRequestTitleBody(ctx context.Context, client *firestore.Client, owner, repo string, number int, title, body string) error {
	if title == "" && body == "" {
		return nil
	}
	docID := fmt.Sprintf("%s_%s_%d", strings.ToLower(owner), strings.ToLower(repo), number)
	prRef := client.Collection("pull_requests").Doc(docID)
	updates := []firestore.Update{
		{Path: "updatedAt", Value: firestore.ServerTimestamp},
	}
	if title != "" {
		updates = append(updates, firestore.Update{Path: "title", Value: title})
	}
	if body != "" {
		updates = append(updates, firestore.Update{Path: "description", Value: body})
	}
	_, err := prRef.Update(ctx, updates)
	return err
}
```

Verify the doc-ID format by looking at `db.CreatePullRequest` — match its scheme exactly.

- [ ] **Step 2: Write CreatePull and UpdatePull handler test cases**

Append to `internal/api/v3/pulls_test.go`:

```go
func TestCreateAndUpdatePull(t *testing.T) {
	// ... full setup: scenario, seed repo, mint token with pull_requests:write perm
	// ... seed two branches in the local repo (main and feature)
	// POST /api/v3/repos/{o}/{r}/pulls
	//   body: {"title":"X","body":"...","head":"feature","base":"main"}
	//   assert 201 + GitHub shape + number + state="open"
	// PATCH /api/v3/repos/{o}/{r}/pulls/{n}
	//   body: {"title":"Y"}; assert 200 + updated title
	// PATCH with body: {"state":"closed"}; assert state="closed"
	// PATCH with no granted perm: 403
}
```

(The test setup is voluminous — model after `pulls_test.go` GET tests + seedLocalRepo from contents_test.go.)

- [ ] **Step 3: Handlers**

Add to `internal/api/v3/pulls.go`:

```go
func (h *V3Handler) CreatePull(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermWrite); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Draft bool   `json:"draft"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}
	if req.Title == "" || req.Head == "" || req.Base == "" {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}

	ic := apps.InstallationContextFrom(r.Context())
	if ic == nil {
		apps.WriteError(w, apps.ErrUnauthorized)
		return
	}

	// Actor: bot user. We pass the bot UID as the AuthorUid and synthesize
	// a username from the app slug. db.CreatePullRequest accepts uid + username.
	pr, err := db.CreatePullRequest(r.Context(), h.FirestoreClient, owner, repo,
		req.Title, req.Body, req.Head, req.Base, ic.BotUserID, "app-"+ic.AppID+"[bot]")
	if err != nil {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}
	apps.WriteJSON(w, http.StatusCreated, pullToFormatter(*pr, owner, repo, h.URLs))
}

func (h *V3Handler) UpdatePull(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermWrite); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	num, _ := strconv.Atoi(chi.URLParam(r, "number"))

	var req struct {
		Title string  `json:"title"`
		Body  *string `json:"body"`
		State string  `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}

	body := ""
	if req.Body != nil {
		body = *req.Body
	}
	if req.Title != "" || body != "" {
		if err := db.UpdatePullRequestTitleBody(r.Context(), h.FirestoreClient, owner, repo, num, req.Title, body); err != nil {
			apps.WriteError(w, err)
			return
		}
	}
	if req.State == "closed" || req.State == "open" {
		if err := db.UpdatePullRequestStatus(r.Context(), h.FirestoreClient, owner, repo, num, req.State); err != nil {
			apps.WriteError(w, err)
			return
		}
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, num)
	if err != nil || pr == nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, pullToFormatter(*pr, owner, repo, h.URLs))
}
```

Add the `"encoding/json"` import.

Add routes:

```go
r.Post("/repos/{owner}/{repo}/pulls", h.CreatePull)
r.Patch("/repos/{owner}/{repo}/pulls/{number}", h.UpdatePull)
```

- [ ] **Step 4: Run tests + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -v ./internal/db/... -v
git add internal/api/v3/pulls.go internal/api/v3/pulls_test.go internal/api/v3/routes.go internal/db/pull_requests.go
git commit -m "feat(v3): POST pulls + PATCH pulls/{n} with bot actor"
```

---

## Task 8: Plan 1 fixup — populate `repositories[]` in token-mint response

**Files:**
- Modify: `internal/apps/handlers.go::CreateInstallationAccessToken`
- Modify: `internal/apps/handlers_test.go` — extend the existing token-mint subtest to assert non-empty repositories array for `selected` installations
- Modify: `internal/apps/testfixtures/fixtures.go` — extend the Scenario to optionally seed a repo

Plan 1's `CreateInstallationAccessToken` returned `"repositories": []interface{}{}` as a placeholder. With v3fmt/repo.go in hand, we can populate it correctly: for `repository_selection == "selected"`, list the repos by ID; for `"all"`, GitHub returns the full list of repos in the installation's account.

- [ ] **Step 1: Extend Scenario to seed a repo**

In `internal/apps/testfixtures/fixtures.go`, add a method:

```go
// SeedRepo creates a repository owned by the installation's account and
// includes it in the installation's repository_ids (switching the
// installation to repository_selection="selected"). Returns the repo name.
func (s *Scenario) SeedRepo(ctx context.Context) (string, error) {
	repoName := "fx-repo-" + randHex(4)
	if err := db.CreateRepositoryMetadata(ctx, s.FS,
		s.Installation.Account.ID, s.Installation.Account.ID,
		repoName, "", "public"); err != nil {
		return "", err
	}
	// Update installation to selected with this one repo.
	repoID := s.Installation.Account.ID + "_" + repoName
	_, err := s.FS.Collection(apps.CollectionInstallations).Doc(s.Installation.InstallationID).Update(ctx, []firestore.Update{
		{Path: "repository_selection", Value: "selected"},
		{Path: "repository_ids", Value: []string{repoID}},
	})
	if err != nil {
		return "", err
	}
	s.Installation.RepositorySelection = "selected"
	s.Installation.RepositoryIDs = []string{repoID}
	return repoName, nil
}
```

Add `"gitbucket/internal/db"` and `"cloud.google.com/go/firestore"` to imports if not already present.

Also extend `Cleanup` to delete seeded repos:

```go
// ... in Cleanup, before the existing deletes:
for _, repoID := range s.Installation.RepositoryIDs {
    parts := strings.SplitN(repoID, "_", 2)
    if len(parts) == 2 {
        _, _ = s.FS.Collection("repositories").Doc(repoID).Delete(ctx)
    }
}
```

- [ ] **Step 2: Modify `CreateInstallationAccessToken`**

In `internal/apps/handlers.go`, replace the hardcoded empty repositories array. The handler currently returns `"repositories": []interface{}{}`. Replace with a lookup that:

1. For `repository_selection == "all"`: query all repos owned by `inst.Account` (use `db.ListUserRepositories(ctx, fs, owner)` if it exists, or skip — `repository_selection: "all"` may legitimately return an empty array, as some App clients only read it for `"selected"` installations).
2. For `repository_selection == "selected"`: look up each `repository_id` in `inst.RepositoryIDs` and format via `v3fmt.RepositoryFromMap`.

```go
import (
    // ...
    "gitbucket/internal/api/v3/v3fmt"
)

// inside CreateInstallationAccessToken, after MintInstallationToken succeeds:
urls := v3fmt.NewURLBuilder(os.Getenv("PUBLIC_BASE_URL"))
var repos []v3fmt.RepositoryDTO
if inst.RepositorySelection == "selected" {
    for _, repoID := range inst.RepositoryIDs {
        // repoID format: "<owner>_<name>"; split and look up.
        parts := strings.SplitN(repoID, "_", 2)
        if len(parts) != 2 {
            continue
        }
        meta, err := db.GetRepositoryMetadata(r.Context(), h.FS, parts[0], parts[1])
        if err != nil || meta == nil {
            continue
        }
        repos = append(repos, v3fmt.RepositoryFromMap(meta, urls))
    }
}
if repos == nil {
    repos = []v3fmt.RepositoryDTO{}
}

WriteJSON(w, 201, map[string]interface{}{
    "token":                out.Plaintext,
    "expires_at":           out.Record.ExpiresAt.UTC().Format(time.RFC3339),
    "permissions":          permissionsJSON(out.Record.Permissions),
    "repository_selection": inst.RepositorySelection,
    "single_file":          nil,
    "repositories":         repos,
})
```

Add imports: `"os"`, `"strings"`, `"gitbucket/internal/api/v3/v3fmt"`, `"gitbucket/internal/db"`.

**Cross-package dependency concern:** `internal/apps` importing `internal/api/v3/v3fmt` creates a dependency arrow `apps → v3fmt`. Since `v3` also depends on `apps` (for middleware + RequirePerm + errors), and now `apps` would depend on `v3fmt`, we need the import graph to stay acyclic. `v3fmt` only imports `apps` for `InstallationContext` in the `UserFromBotContext` helper. If you wrote `UserFromBotContext` to take only the BotUserID + AppID strings (not the whole context), `v3fmt` doesn't need to import `apps` at all and the cycle is broken.

**Refactor before Step 2 if `v3fmt` imports `apps`:** simplify `UserFromBotContext` to take primitive arguments:

```go
// In v3fmt/user.go, change signature:
func UserFromBot(appID, botUserID string, urls *URLBuilder) UserDTO {
	return User(staticUserSource{
		login: "app-" + appID + "[bot]",
		uid:   "user:" + botUserID,
		typ:   "Bot",
	}, urls)
}
```

Remove the `"gitbucket/internal/apps"` import from `v3fmt/user.go`. Update any callers in `internal/api/v3` to pass `ic.AppID` + `ic.BotUserID` explicitly.

- [ ] **Step 3: Test it**

In `internal/apps/handlers_test.go`, extend the existing `TestAppHandlersEndToEnd/POST .../access_tokens mints a token` subtest:

```go
// Inside the existing token-mint subtest, after asserting status 201:
repos, ok := body["repositories"].([]interface{})
if !ok {
    t.Errorf("repositories field missing or wrong type: %v", body["repositories"])
}
// For the default Scenario (RepositorySelection=all), this stays empty.
// To test selected, use Scenario.SeedRepo before minting.
_ = repos
```

Add a new test that uses `SeedRepo`:

```go
func TestTokenMintPopulatesRepositoriesForSelected(t *testing.T) {
    // ... set up Scenario, call scen.SeedRepo(ctx) to switch to "selected" with one repo
    // ... mint token via the HTTP endpoint
    // assert body["repositories"] has 1 entry with the correct name
}
```

- [ ] **Step 4: Run all tests + build + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/... -v 2>&1 | tail -40
go build ./...
git add internal/apps/handlers.go internal/apps/handlers_test.go internal/apps/testfixtures/fixtures.go internal/api/v3/v3fmt/user.go internal/api/v3/pulls.go
git commit -m "fix(apps): populate repositories[] in token-mint response (Plan 1 follow-up)"
```

---

## Task 9: End-to-end fake-App walkthrough

**Files:**
- Create: `internal/api/v3/e2e_test.go`

A single test that simulates an App's actual usage: mint installation token via the JWT endpoint, then use it on a probe sequence — GET repo, GET contents, GET pulls, POST a PR. Assert each response shape end-to-end.

- [ ] **Step 1: Write the test**

```go
// internal/api/v3/e2e_test.go
package v3_test

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

	"gitbucket/internal/api/v3"
	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestPlan2FakeAppWalkthrough(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	repoName, err := scen.SeedRepo(ctx)
	if err != nil {
		t.Fatalf("SeedRepo: %v", err)
	}
	// Also seed a bare local repo so contents/refs/trees work.
	tmp := t.TempDir()
	owner := scen.Installation.Account.ID
	seedBareRepo(t, tmp, owner, repoName) // copy seedLocalRepo logic from contents_test.go

	r := chi.NewRouter()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	appsH := apps.NewHandler(fs, scen.Store, jwtV)
	apps.RegisterRoutes(r, appsH)
	v3H := v3.NewV3Handler(fs, nil, "https://test.gitbucket.local")
	v3H.LocalReposRoot = tmp
	v3.RegisterV3Routes(r, v3H)

	// Step 1: mint installation token via the JWT-authed endpoint.
	jwtStr := scen.SignJWT(t)
	req := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("mint: code = %d body: %s", rr.Code, rr.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &minted)
	tok := minted["token"].(string)
	repos := minted["repositories"].([]interface{})
	if len(repos) != 1 {
		t.Fatalf("repositories array len = %d, want 1 (Plan 1 fixup)", len(repos))
	}

	// Step 2: GET the repo.
	req = httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("get repo: %d %s", rr.Code, rr.Body.String())
	}

	// Step 3: GET contents/README.md.
	req = httptest.NewRequest("GET",
		"/api/v3/repos/"+owner+"/"+repoName+"/contents/README.md?ref=main", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("get contents: %d %s", rr.Code, rr.Body.String())
	}

	// Step 4: GET pulls (empty list initially).
	req = httptest.NewRequest("GET",
		"/api/v3/repos/"+owner+"/"+repoName+"/pulls", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("list pulls: %d %s", rr.Code, rr.Body.String())
	}

	// Step 5: Create a PR (requires a 'feature' branch in the local repo).
	// Add this branch in seedBareRepo, then:
	body := bytes.NewBufferString(`{"title":"Test PR","body":"From Plan 2 e2e","head":"feature","base":"main"}`)
	req = httptest.NewRequest("POST",
		"/api/v3/repos/"+owner+"/"+repoName+"/pulls", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create pull: %d %s", rr.Code, rr.Body.String())
	}
	var pr map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &pr)
	if pr["user"].(map[string]interface{})["type"] != "Bot" {
		t.Errorf("created PR user.type = %v, want Bot", pr["user"])
	}
}

func seedBareRepo(t *testing.T, root, owner, repo string) {
	// Same as seedLocalRepo in contents_test.go but also creates a 'feature' branch.
	// Duplicate the logic here; if it grows further, factor into a shared test helper.
}
```

- [ ] **Step 2: Run + commit**

```bash
FIRESTORE_EMULATOR_HOST=localhost:8084 go test ./internal/api/v3/... -run TestPlan2FakeAppWalkthrough -v
git add internal/api/v3/e2e_test.go
git commit -m "test(v3): end-to-end fake-App walkthrough (mint→browse→PR)"
```

---

## Self-Review

### Spec coverage

Plan 2 covers spec §6 sections:

- §6.1 endpoint set — partial (8 of 14): repo, contents, ref, trees, pulls list/get/create/update. Issues + issue-comments + the 2 manifest endpoints are explicitly deferred.
- §6.2 handler shape — Task 3 example follows the spec's pattern (RequirePerm + service call + v3fmt).
- §6.3 translation layer — `internal/api/v3/v3fmt/` package with one file per resource type.
- §6.4 GitHub quirks: `id` as int64 derived from StableID (Task 1) — ✓; `node_id` base64 of `<Type>:<id>` (Task 2) — ✓; URLs via URLBuilder (Task 1) — ✓; `user` blocks with login/id/avatar_url/type/site_admin (Task 2) — ✓; ISO 8601 UTC Z suffix (formatTime in Task 3) — ✓; 404 vs 403 (handlers use ErrNotFound for missing-or-out-of-scope) — ✓.

Plus the Plan 1 follow-up: Task 8 fills the empty `repositories[]` array.

### Placeholder scan

The only "TBD"-shaped item in the plan is in Task 4 Step 1's `MaterializeRepo` placeholder, which is intentional and documented with a research instruction: the implementer reads `internal/api/api.go`'s existing materialization to decide whether to expose-or-duplicate. Task 4 Step 9 replaces the stub with the real implementation. This is acceptable because the materialization shape genuinely depends on how the existing code is structured — pre-specifying it would risk pretending knowledge the plan can't validate.

### Type consistency

- `UserSource` interface defined Task 2, used in Tasks 3-7. The interface methods (`GetLogin`, `GetUserKey`, `GetType`, `GetAvatarURL`) are referenced consistently. Task 8 mentions `UserFromBotContext` → renamed to `UserFromBot(appID, botUserID, urls)` to break the apps→v3fmt cycle.
- `URLBuilder` constructed once via `NewURLBuilder`, passed to every formatter. Methods (`APIRoot`, `RepoAPI`, `PullAPI`, etc.) are referenced consistently.
- `StableID(key string) int64` returns positive int64; consistent across all `encodeNodeID(...)` callers.
- `MaterializeRepo` signature `(ctx, fs, sc, owner, repo, localReposRoot) (string, error)` — same throughout (Tasks 4, 5).
- `RepoSource`, `PullRequestSource`, `ContentFileSource`, `ContentDirSource`, `RefSource`, `TreeSource`, `TreeEntrySource` — each defined in their respective formatter file and used by the corresponding handler.
- `V3Handler` struct fields (`FirestoreClient`, `StorageClient`, `URLs`, `LocalReposRoot`) — set in `NewV3Handler` (Task 1) + `LocalReposRoot` added in Task 4 Step 8, then referenced uniformly.

No drift detected.

### Scope check

Plan 2 ships GitHub-shape REST for 8 endpoints plus the Plan 1 fixup — coherent, self-contained, independently demoable. The deferred 6 endpoints (issues + comments) have no existing backend, so deferring them is a design choice (a separate Plan 2.5 will handle them). The webhook engine (Plan 3) and manifest UI (Plan 4) follow.

---

## Execution notes

- Stack this plan on top of branch `feature/github-app-emulation-foundation` (Plan 1) since Plan 1 hasn't merged to main yet. Branch name suggestion: `feature/github-app-emulation-rest-surface`. If Plan 1 merges first, rebase onto main.
- Firestore emulator on `localhost:8084` is required for the integration tests.
- All handlers use `apps.RequirePerm` to gate access; the default `Scenario` in `testfixtures` grants all four base permissions (issues:write, contents:write, pull_requests:write, metadata:read) — sufficient for every Plan 2 test.
- Cross-package imports kept acyclic: `internal/api/v3` → `internal/apps` (for middleware/errors), `internal/api/v3` → `internal/api/v3/v3fmt` (formatters), `internal/api/v3/v3fmt` → ∅ (no project imports). Task 8 introduces `internal/apps` → `internal/api/v3/v3fmt` for the token-mint repositories field — that's the only cross-direction edge, and v3fmt has no apps import so no cycle forms.
