# GitHub App Emulation — MVP Design

**Date:** 2026-05-20
**Status:** Design
**Scope:** First slice toward GitHub App compatibility — just enough to let Claude Code (and similarly Jules) connect to GitBucket via the standard GitHub App manifest flow, mint installation tokens, read/write issues and comments and PRs, and receive signed webhooks when those resources change.

---

## 1. Goals & Non-Goals

### Goals

1. A third-party agent that already speaks GitHub Apps (Claude Code, Jules, future ones like SonarQube/Snyk) can point at a GitBucket instance, walk through the manifest registration flow, install onto a user's repos, and operate end-to-end without any tool-specific glue.
2. The "@claude on an issue" loop works: an issue comment fires a webhook to Claude, Claude reads the issue and writes a comment back via the REST API, and that comment appears in GitBucket attributed to a synthetic `claude-code[bot]` user.
3. No regressions for the existing SPA + Git CLI flows. The new surface is additive.

### Non-Goals (deferred to follow-on specs)

- User-to-server OAuth (`/login/oauth/authorize` + `/login/oauth/access_token`). Apps can't act *as a user* yet — only as the bot identity tied to their installation.
- The full GitHub REST surface. We implement ~18 endpoints, not the hundreds GitHub exposes.
- Checks API, statuses, reviews, branch protection v3 surface (data exists in GitBucket; the v3 formatters are a follow-on).
- GraphQL, Actions, releases, gists, packages.
- Admin UI for delivery logs, suspension, secret rotation, manual redelivery. The `webhook_deliveries` audit log makes these straightforward follow-ons.
- Multi-region or cross-region delivery.

### Out-of-scope by intent

- An "orgs" concept. We avoid the corner without paying its cost — see §3.

---

## 2. Architecture Overview

A new subsystem under `internal/apps/` (mirror of `internal/auth/`) plus a new GitHub-shape REST namespace under `/api/v3/*`. Five components:

```
┌─────────────────────────────────────────────────────────────────────┐
│  /settings/apps/new (Manifest flow) ──► internal/apps/manifest.go   │
│                                              │                       │
│                                              ▼                       │
│  Firestore: apps/{app_id}      ◄── stores App row + Secret Manager  │
│  Firestore: installations/...        resource names for private key │
│                                       and webhook secret             │
│                                                                      │
│  Inbound from Claude/Jules:                                          │
│    POST /api/v3/app/installations/{id}/access_tokens                 │
│      ──► internal/apps/jwt.go (verify RS256 against stored pubkey)  │
│      ──► internal/apps/tokens.go (mint ghs_..., store w/ TTL)       │
│                                                                      │
│    GET /api/v3/repos/{owner}/{repo}/issues/{n}  (and ~17 others)    │
│                                                                      │
│    Plus 2 manifest-conversion endpoints under /api/v3/...            │
│      ──► InstallationTokenAuth middleware                           │
│      ──► internal/api/v3/*.go handlers (call existing services,     │
│                                          format to GitHub shape)    │
│                                                                      │
│  Outbound to Claude/Jules:                                           │
│    Existing handlers (issues, comments, PRs, git push) fire events  │
│    via internal/apps/events.go ──► Cloud Tasks queue                │
│      ──► /_internal/dispatch-webhook/{delivery_id} (own service)    │
│      ──► HTTPS POST to App's webhook URL (HMAC-signed)              │
└─────────────────────────────────────────────────────────────────────┘
```

### Key architectural choices

- **API surface:** Parallel `/api/v3/` namespace, not a reshape of existing `/api/*`. The existing SPA-facing API and the new GitHub-shape API live side by side. Translation happens in a thin formatter layer over shared service functions. Avoids touching the frontend, the existing test suite, or the PROJECT.md interface contracts.
- **Private keys live in Secret Manager**, not Firestore. App's Firestore row holds only the secret resource name (`projects/.../secrets/app-{id}-private-key/versions/latest`). Public key is duplicated into Firestore for fast JWT verify on the hot path.
- **Webhook secrets live in Secret Manager** for the same reason — plaintext is needed to compute HMAC signatures on every event fire. Per-process in-memory cache with 5-minute TTL.
- **Installation tokens live in Firestore** with TTL policy. We store the SHA-256 hash as the document ID; plaintext is returned exactly once at issuance and never persisted.
- **Cloud Tasks queue for webhook delivery.** Tasks targets our own `/_internal/dispatch-webhook/{id}` endpoint so we keep full delivery visibility (response codes, durations) in the `webhook_deliveries` audit log. Sub-millisecond extra hop inside GCP.
- **Synthetic `<slug>[bot]` user per App,** auto-created at registration. Existing `knownSyncIdentities` loop-prevention map is replaced by a Firestore-backed view so new Apps integrate without code deploys.

---

## 3. Ownership Model

GitBucket only has users today; orgs are a likely future need. We model installations and App ownership with a **polymorphic `account` reference** from day one to avoid painting ourselves into a corner without paying the cost of building orgs now.

Every installation and App-ownership record carries:

```
account: { id: string, type: "User" }
```

In MVP, `type` is always `"User"`. Adding orgs later means:

- Add an `accounts` collection (or a `type` discriminator on user records).
- Set `account.type = "Organization"` for org-scoped installs.
- App schema, installation schema, token issuance, and webhook delivery code stay unchanged.

The trap this avoids is conflating "the human who installed the App" with "the account scope the App runs against." GitHub's model already separates them; we copy that separation.

---

## 4. Data Model

Four new Firestore collections.

```
apps/{app_id}
  app_id:                    string  (random; also serves as "App ID" returned to clients)
  slug:                      string  (e.g. "claude-code"; unique)
  name:                      string  (human-readable display name)
  owner_account:             { id: string, type: "User" }
  bot_user_id:               string  (FK to users/{...}, the "{slug}[bot]" user)
  client_id:                 string  (returned at registration; for future OAuth)
  client_secret_hash:        string  (sha256; plaintext returned only once)
  webhook_url:               string
  webhook_secret_resource:   string  (Secret Manager resource name)
  webhook_secret_hash:       string  (sha256; shown once for self-verification)
  private_key_secret:        string  (Secret Manager resource name)
  public_key_pem:            string  (duplicated for fast JWT verify)
  default_permissions:       { contents: "write", issues: "write",
                                pull_requests: "write", metadata: "read" }
  default_events:            ["issues", "issue_comment",
                                "pull_request", "pull_request_review_comment",
                                "push", "installation"]
  suspended_at:              timestamp | null
  created_at:                timestamp

installations/{installation_id}
  installation_id:           string
  app_id:                    string  (FK to apps/{...})
  account:                   { id: string, type: "User" }
  repository_selection:      "all" | "selected"
  repository_ids:            []string  (only when selection == "selected")
  permissions:               { contents: "write", ... }  (may be narrower than App defaults)
  events:                    ["issues", "issue_comment", ...]
  suspended_at:              timestamp | null
  created_at:                timestamp

installation_tokens/{token_sha256}
  installation_id:           string
  permissions:               { ... }      (snapshot at issuance; may subset installation perms)
  repository_ids:            []string     (snapshot; may subset installation repos)
  issued_at:                 timestamp
  expires_at:                timestamp    (exactly issued_at + 1h; Firestore TTL on this field)

webhook_deliveries/{delivery_id}        (audit log; Cloud Tasks does the actual queueing)
  delivery_id:               string  (UUID; also sent as X-GitHub-Delivery header)
  app_id:                    string
  installation_id:           string
  event:                     string  (e.g. "issues")
  payload_sha256:            string  (de-dup / forensics; payload itself not stored)
  target_url:                string
  status:                    "pending" | "delivered" | "failed"
  attempts:                  int
  last_response_code:        int | null
  last_attempted_at:         timestamp
  created_at:                timestamp
```

Notes:

- **No plaintext secrets at rest in Firestore.** Client secret is sha256-only (only verified, never used to sign). Webhook secret and private key are in Secret Manager. Installation token is hashed and the hash is the doc ID.
- **Token lookup is O(1).** Validation hashes the inbound token and does a single point-read by doc ID. No queries, no scans.
- **Firestore native TTL** cleans up expired `installation_tokens` and `webhook_deliveries` (set retention to 30 days for the latter). Configured once via `gcloud firestore fields ttls update expires_at --collection-group=installation_tokens`.

---

## 5. Auth Plane

Three layers, all in `internal/apps/`.

### 5.1 JWT verification (`internal/apps/jwt.go`)

Apps sign a JWT with their private key. We accept:

- `alg = RS256` only.
- `iss = <app_id>` — used to look up the App's `public_key_pem`.
- `iat` ≤ now (within 30s clock-skew tolerance).
- `exp` > now (within 30s clock-skew tolerance).
- `exp - iat` ≤ 10 minutes (matches GitHub).
- App not suspended.

Verification reads `apps/{iss}` once (cached in-process for 60s by `app_id`). Failures return `401 Unauthorized` with a GitHub-shape error body.

### 5.2 Installation token issuance (`internal/apps/tokens.go`)

Handler for `POST /api/v3/app/installations/{id}/access_tokens`:

```
1. App JWT in Authorization: Bearer ... → verify (5.1)
2. Load installations/{id}; 404 if not owned by JWT's app_id
3. Parse optional body: { permissions?: {...}, repository_ids?: [...] }
   - Must be a subset of installation.permissions and installation.repository_ids
   - 422 with GitHub-shape error if it requests anything broader
4. Mint token:
     plaintext = "ghs_" + base62(32 random bytes)   // ~36 chars
     hash      = sha256(plaintext)
5. Write installation_tokens/{hash}:
     installation_id, issued_at = now, expires_at = now + 1h,
     permissions = (request body | installation.permissions),
     repository_ids = (request body | installation.repository_ids)
6. Return 201 with GitHub-shape body:
     {
       "token": "<plaintext>",
       "expires_at": "<ISO8601 UTC with Z>",
       "permissions": { ... },
       "repository_selection": "all" | "selected",
       "repositories": [ ...full repo list, only if selection == "selected"... ],
       "single_file": null
     }
   Response headers:
     HTTP/1.1 201 Created
     Content-Type: application/json; charset=utf-8
     Cache-Control: no-store, no-cache
     X-GitHub-Media-Type: github.v3; format=json
```

### 5.3 Request auth middleware (`internal/apps/middleware.go`)

Mounted on the entire `/api/v3/*` chi subrouter, plus on `/r/{owner}/{repo}.git/*` for Git CLI use:

```
1. Parse Authorization: Bearer <token>, header X-Access-Token, or HTTP Basic
   (user = "x-access-token", pass = <token>) — Git CLI uses Basic.
2. If token starts with "ghs_":
     hash = sha256(token); point-read installation_tokens/{hash}
3. 401 if missing, not found, or `expires_at <= now`. (Firestore TTL is a periodic sweep, not real-time — the middleware must compare `expires_at` to now explicitly; it cannot rely on the doc being deleted.)
4. Inject InstallationContext into r.Context():
     { installation_id, app_id, permissions, repository_ids, bot_user_id }
5. Downstream handlers call apps.RequirePerm(ctx, "issues", apps.Write) to gate access.
   Permission failure returns 403 with GitHub-shape body.
6. Write handlers set actor = bot_user_id when persisting (issues, comments, PRs),
   so the synthetic [bot] user appears as the author.
```

**Coexistence with existing auth.** This middleware is additive. The existing `RequireWebAuth` (Firebase ID tokens) and PAT auth still run on `/api/*` and `/r/.../git/*` exactly as today. `/api/v3/*` is the only place the new middleware runs, so installation tokens cannot leak into the Firebase-auth code path.

**Permission helper.** A single helper keeps handlers terse:

```go
if err := apps.RequirePerm(r.Context(), "issues", apps.Write); err != nil {
    apps.WriteError(w, err)  // 403, GitHub-shape body
    return
}
```

---

## 6. GitHub-Compatible REST Surface (`/api/v3/*`)

A new chi subrouter mounted in `main.go`, handlers under `internal/api/v3/`. The endpoint set is the minimum Claude Code and Jules actually call.

### 6.1 Endpoint set (MVP)

```
App identity & token issuance
  GET    /api/v3/app                                          — App metadata (whoami for JWT)
  GET    /api/v3/app/installations                            — installations for this App
  GET    /api/v3/app/installations/{id}                       — one installation
  POST   /api/v3/app/installations/{id}/access_tokens         — mint installation token

Repository
  GET    /api/v3/repos/{owner}/{repo}                         — repo metadata
  GET    /api/v3/repos/{owner}/{repo}/contents/{path}         — file or dir listing
  GET    /api/v3/repos/{owner}/{repo}/git/ref/{ref}           — resolve ref to SHA
  GET    /api/v3/repos/{owner}/{repo}/git/trees/{sha}         — tree listing

Issues
  GET    /api/v3/repos/{owner}/{repo}/issues
  GET    /api/v3/repos/{owner}/{repo}/issues/{n}
  POST   /api/v3/repos/{owner}/{repo}/issues
  PATCH  /api/v3/repos/{owner}/{repo}/issues/{n}
  GET    /api/v3/repos/{owner}/{repo}/issues/{n}/comments
  POST   /api/v3/repos/{owner}/{repo}/issues/{n}/comments

Pull Requests
  GET    /api/v3/repos/{owner}/{repo}/pulls
  GET    /api/v3/repos/{owner}/{repo}/pulls/{n}
  POST   /api/v3/repos/{owner}/{repo}/pulls
  PATCH  /api/v3/repos/{owner}/{repo}/pulls/{n}

App manifest registration (web flow, see §8)
  POST   /api/v3/settings/apps/manifest-conversions
  POST   /api/v3/app-manifests/{code}/conversions
```

18 data endpoints + 2 manifest-conversion endpoints (covered in §8) = 20 total. Each handler is a thin formatter on top of services that already exist in `internal/api/` (issues, comments, pull_requests, the browse helpers in `git.go`).

### 6.2 Handler shape

```go
// internal/api/v3/issues.go
func (h *V3Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
    if err := apps.RequirePerm(r.Context(), "issues", apps.Read); err != nil {
        apps.WriteError(w, err); return
    }
    owner := chi.URLParam(r, "owner")
    repo  := chi.URLParam(r, "repo")
    num   := chi.URLParam(r, "number")

    issue, err := h.Issues.Get(r.Context(), owner, repo, num)
    if err != nil { apps.WriteError(w, err); return }

    apps.WriteJSON(w, 200, v3fmt.Issue(issue, h.URLBuilder))
}
```

### 6.3 Translation layer (`internal/api/v3/v3fmt/`)

The only place that knows GitHub's field shapes. One file per resource type: `issue.go`, `comment.go`, `pull.go`, `repo.go`, `user.go`, `contents.go`, `ref.go`. Each exports pure functions:

```go
func Issue(src *issues.Issue, urls *URLBuilder) IssueDTO { ... }
func Comment(src *comments.Comment, urls *URLBuilder) CommentDTO { ... }
```

Pure functions are trivially unit-testable — table-driven tests assert exact JSON against golden fixtures (real, sanitized GitHub responses).

### 6.4 GitHub quirks the formatters must get right

- **`id` is an integer.** Firestore IDs are strings. The formatter assigns a stable `int64` (derived from a hash of the Firestore doc ID, stored back to the doc on first read so the same entity always renders the same numeric `id`).
- **`node_id` is base64.** Computed in the formatter as `base64("<Type>:<id>")` (e.g. `MDU6SXNzdWUx...`). Never stored.
- **URLs.** Every `url`, `html_url`, `comments_url`, `pull_request.diff_url`, etc. is built by a `URLBuilder` from the configured GitBucket base URL. No hard-coded `https://api.github.com` strings anywhere.
- **`user` blocks.** Always include `login`, `id`, `avatar_url`, `type`, `site_admin`, plus the canonical URL set. The synthetic bot user renders with `type: "Bot"` and `login: "{slug}[bot]"`.
- **Timestamps.** ISO 8601 UTC with `Z` suffix. Never offsets, never local.
- **Error shape.** `{ "message": "...", "documentation_url": "..." }` with appropriate status codes.
- **404 vs 403 on private repos.** Always 404 if the installation can't see the resource — 403 would leak existence.

---

## 7. Outbound Webhook Engine

When a user opens an issue, comments, opens/updates a PR, or pushes to a ref, every installed App subscribed to that event receives a signed HTTPS POST. Cloud Tasks is the queue; GitBucket enqueues and signs.

### 7.1 Where events originate

Existing handlers already know when these things happen — `issues.go` Create/Patch, `pull_requests.go` Open/Update, the comment endpoints, and the post-receive sync path in `git.go`. We add one call at the end of each successful write:

```go
events.Fire(ctx, events.IssueOpened, events.IssuePayload{
    Owner: o, Repo: r, Issue: issue,
})
```

`events.Fire` is non-blocking — it returns after enqueueing. Failure to enqueue is logged but does not fail the user-facing request (the issue is already created; webhook delivery is best-effort with retry).

### 7.2 `internal/apps/events.go` — the fire path

```
Fire(ctx, eventType, payload):
  1. If sender is a `{slug}[bot]` user → return (loop prevention, see §7.4).
  2. Query installations where:
       account matches the repo owner
       AND (repository_selection == "all"  OR  repo_id ∈ repository_ids)
       AND eventType ∈ events
       AND suspended_at IS NULL
       AND app not suspended
  3. For each matching installation:
       a. Build the GitHub-shape JSON payload via v3fmt
            + "installation": { "id": ..., "node_id": ... }
            + "sender": user who triggered the event
       b. Fetch webhook_secret from Secret Manager (5-min in-process cache).
       c. Compute X-Hub-Signature-256 = "sha256=" + hex(HMAC-SHA256(secret, payload))
          and the rest of the GitHub-shape headers (X-GitHub-Event, X-GitHub-Delivery,
          X-GitHub-Hook-ID, X-GitHub-Hook-Installation-Target-Type / -ID).
       d. Write webhook_deliveries/{uuid} (status=pending, payload_sha256, target_url).
       e. Enqueue Cloud Task with the signed payload + headers in the task body:
            HTTP POST → https://<our-host>/_internal/dispatch-webhook/{delivery_id}
            Task body (JSON): { "headers": { ... }, "payload": "<raw json bytes>" }
            OIDC token bound to service account (verified at the dispatcher).
```

### 7.3 The dispatcher (`/_internal/dispatch-webhook/{id}`)

The Cloud Task target. Reasons we proxy through our own service instead of having Tasks call the App directly:

- We see every response code and duration, populating `webhook_deliveries.status` and `last_response_code` accurately.
- We can apply our own per-delivery logic (e.g. drop if the App was suspended after the task was enqueued).
- Replay/redeliver later can re-enqueue from the stored delivery row.

Dispatcher flow:

```
1. Verify Cloud Tasks OIDC token (audience = configured dispatcher URL).
2. Load webhook_deliveries/{id}; abort 200 if already delivered or if the
   App / installation was suspended after enqueue (do not retry).
3. Read headers + payload from the task body (signed once at enqueue time
   in events.go step 3c; the dispatcher never re-signs).
4. HTTPS POST to delivery.target_url, relaying the body verbatim with:
     Content-Type: application/json
     User-Agent: GitBucket-Hookshot/1.0
     X-GitHub-Event: <event>
     X-GitHub-Delivery: <delivery_id>
     X-Hub-Signature-256: sha256=<hex>
     X-GitHub-Hook-ID: <app_id>
     X-GitHub-Hook-Installation-Target-Type: repository
     X-GitHub-Hook-Installation-Target-ID: <repo_id>
5. Update delivery row: status, attempts++, last_response_code, last_attempted_at.
6. Non-2xx → return non-2xx so Cloud Tasks retries.
```

**Payload sizing.** Cloud Tasks HTTP-target task bodies are capped at 1 MB. Issue, comment, and PR webhook payloads are well under this; the only concern is `push` events with very large commit lists. If a push payload exceeds 800 KB we truncate the `commits` array to the last 20 commits and set `truncated: true` in the payload (matching GitHub's documented behavior).

Queue-level retry config:

```
maxAttempts: 5
minBackoff: 10s
maxBackoff: 1h
maxRetryDuration: 24h
```

### 7.4 Loop prevention

When `sender` is one of the synthetic `{slug}[bot]` users, the event is **not enqueued** — the bot identity check happens in `events.Fire` step 1, before installation matching. This complements the existing inbound loop prevention in `webhooks.go` (which discards pushes from sync bots and pushes carrying a `Synced-By: GitBucket` trailer). Together the two layers ensure App ↔ App cycles can't form.

### 7.5 Replay & redeliver

Out of scope for MVP. `webhook_deliveries` retains enough context (target URL, payload hash, headers) that a follow-on admin UI can add a "redeliver" button that re-enqueues a Task. The payload itself is not stored, but it's deterministically reconstructable from the underlying entity (issue, comment, etc.) — by design, since most webhook payloads are just the formatted current state of the resource.

---

## 8. App Manifest Registration Flow

GitHub's manifest flow is a three-hop browser dance. We mirror it exactly so Claude's and Jules's existing setup logic works unchanged.

### 8.1 The three hops

**Hop 1 — manifest landing.** Claude redirects the admin's browser to:

```
GET /settings/apps/new?manifest=<URL-encoded JSON>
```

where the manifest JSON is the shape:

```json
{
  "name": "Claude Code",
  "url": "https://claude.ai",
  "hook_attributes": { "url": "https://api.anthropic.com/.../webhook" },
  "redirect_url": "https://claude.ai/setup/callback",
  "callback_urls": ["https://claude.ai/oauth/callback"],
  "public": false,
  "default_permissions": { "contents": "write", "issues": "write",
                            "pull_requests": "write", "metadata": "read" },
  "default_events": ["issues", "issue_comment", "pull_request"]
}
```

GitBucket serves an SPA route that:

- Requires Firebase web auth (the logged-in user becomes the App owner).
- Shows the manifest in a confirmation card: name, URL, permissions table, events, webhook URL.
- Presents one button: **"Create App on behalf of {logged-in user}"**.

**Hop 2 — manifest conversion.** On confirm, the frontend POSTs:

```
POST /api/v3/settings/apps/manifest-conversions
Body: { manifest: <JSON>, code: <random one-time code> }
```

Backend:

```
1. Require Firebase web auth.
2. Validate manifest (required fields, permission values, event names, URL shapes).
3. Allocate slug from manifest.name (kebab-cased, suffixed if collision).
4. Generate: app_id, client_id, client_secret (plaintext),
             webhook_secret (plaintext), RSA-2048 keypair.
5. Store private key in Secret Manager   → apps/{app_id}/private-key
   Store webhook_secret in Secret Manager → apps/{app_id}/webhook-secret
6. Write apps/{app_id} Firestore doc (per §4).
7. Auto-create synthetic bot user: users/{slug}[bot] (per §9).
8. Store ManifestConversion{code → app_id} in Firestore with 10-minute TTL.
9. Return { redirect_url: "<manifest.redirect_url>?code=<code>" }.
```

Frontend redirects the browser to `redirect_url`.

**Hop 3 — Claude exchanges the code.** Claude's callback handler hits:

```
POST /api/v3/app-manifests/{code}/conversions
```

GitBucket:

```
1. Look up ManifestConversion → app_id (404 if expired or already used).
2. Return GitHub-shape body with everything Claude needs to operate:
     {
       "id": <app_id>,
       "slug": "claude-code",
       "name": "Claude Code",
       "owner": { ...user... },
       "client_id": "...",
       "client_secret": "<plaintext>",   ← only time it appears
       "webhook_secret": "<plaintext>",  ← only time it appears
       "pem": "-----BEGIN RSA PRIVATE KEY-----\n...",  ← only time it appears
       "html_url": "https://gitbucket.../apps/claude-code",
       "permissions": { ... },
       "events": [ ... ]
     }
3. Delete the ManifestConversion doc (single-use).
```

### 8.2 Installation flow (separate from registration)

Once an App exists, a user installs it onto their repos:

```
GET /settings/apps/{slug}/installations/new
  SPA page with repo picker:
    "Install Claude Code"
    [○ All repositories]
    [● Only select repositories: ☑ repo-a  ☐ repo-b ...]

POST /api/v3/user/installations
  Body: { app_id, repository_selection, repository_ids[] }
  Backend:
    1. RequireWebAuth.
    2. Write installations/{installation_id}.
    3. Fire "installation" webhook event to App with action: "created".
    4. Return redirect_url back to App's setup_url (if registered).
```

### 8.3 Who can register / install?

- **Register:** any authenticated GitBucket user. No admin gate in MVP — same trust level as creating a repo.
- **Install:** the account owner only. Future org support adds "org admins" to this set.

### 8.4 UX surface for MVP

Three new SPA routes:

- `/settings/apps/new` — manifest confirmation card.
- `/settings/apps/{slug}/installations/new` — install repo picker.
- `/settings/apps` — read-only list of apps the user owns + apps installed on their account.

The full admin UI (delivery logs, suspend, rotate secrets, manual redeliver) is a follow-on spec.

---

## 9. Bot User Lifecycle & Loop Prevention

The synthetic `{slug}[bot]` user is a normal `users/{id}` document with three guard fields:

```
users/{id}
  username:         "claude-code[bot]"
  display_name:     "Claude Code"
  email:            "claude-code[bot]@bots.gitbucket.local"   (synthetic, non-routable)
  avatar_url:       <copied from App's manifest, or default bot avatar>
  type:             "Bot"           ← NEW field; existing users are implicitly "User"
  owning_app_id:    <app_id>        ← NEW; non-null only for bot users
  created_at:       timestamp
```

No password, no Firebase UID, no PATs.

### 9.1 Username regex change

The existing username regex `^[a-zA-Z0-9-]{3,20}$` rejects `claude-code[bot]` (brackets). We change it to:

```
^[a-zA-Z0-9-]{3,20}(\[bot\])?$
```

The `[bot]` suffix form is reserved — only the App registration path can create it. User-facing signup still rejects brackets explicitly (validator-level guard, not regex-level). This is explicit and visible in webhook payloads where third-party tools expect the literal `[bot]` suffix in `sender.login`.

### 9.2 Loop prevention integration

The existing `webhooks.go` has a hard-coded `knownSyncIdentities` map. We replace it with a Firestore-backed view:

```go
// internal/apps/bots.go
func IsBotIdentity(ctx context.Context, login string) bool {
    // Cache: well-known sync bots + every users/{id} with type:"Bot",
    // refreshed every 60s
    return botIdentityCache.Load().Contains(strings.ToLower(login))
}

// internal/api/webhooks.go
- if knownSyncIdentities[strings.ToLower(payload.Sender.Login)] || ...
+ if apps.IsBotIdentity(r.Context(), payload.Sender.Login) || ...
```

Cache sources: (a) the existing hard-coded sync-bot list, kept as a baked-in seed; (b) every `users/{id}` doc with `type: "Bot"`. New App registration → bot user created → cache picks it up on the next 60s refresh. No code deploy needed when a new App is added.

### 9.3 Outbound symmetry

Already covered in §7.4 — `events.Fire` skips installations when `sender` is a bot. Inbound (sync.go) and outbound (events.go) together close the App ↔ App cycle.

### 9.4 Bot users cannot be impersonated

- No login path accepts a bot username.
- No PAT can be created for a bot.
- The existing `/api/*` (web) routes reject `type: "Bot"` from any write attempt at the user resolver layer (not at handler call sites), so a stolen Firebase token still can't act as a bot.

---

## 10. Testing Strategy

Three layers, escalating from cheap-and-fast to slow-and-high-fidelity.

### 10.1 Layer 1 — pure unit tests

The v3 formatters under `internal/api/v3/v3fmt/` are pure functions. Tests assert exact-bytes equality against golden JSON files captured from real GitHub responses (sanitized):

```
internal/api/v3/v3fmt/testdata/
  issue.golden.json
  comment.golden.json
  pull.golden.json
  repo.golden.json
  contents-file.golden.json
  contents-dir.golden.json
  ref.golden.json
  bot-user.golden.json
```

JWT verify tests (`internal/apps/jwt_test.go`): valid RS256, wrong key, expired `exp`, `exp` more than 10 minutes after `iat`, missing claims, suspended App, clock-skew tolerance.

Token minting tests: `ghs_` prefix, length, hash-as-doc-id behavior, permission/repo subset enforcement on the optional request body.

### 10.2 Layer 2 — Firestore-emulator integration tests

Same pattern as existing `internal/api/*_test.go`. New files:

```
internal/apps/manifest_test.go     — full 3-hop conversion, including expired/used codes
internal/apps/tokens_test.go       — mint → store → middleware reads it back → 401 after expiry
internal/apps/middleware_test.go   — header parsing (Bearer / x-access-token / Basic),
                                      permission denial returns GitHub-shape 403,
                                      bot user injection into request context
internal/apps/events_test.go       — installation matching query (subscription filtering),
                                      sender=bot suppression, HMAC signature correctness
internal/apps/bots_test.go         — bot cache refresh, regex carve-out, loop prevention
internal/api/v3/issues_test.go     — end-to-end: create issue via /api/v3 → assert that
                                      the same issue is readable via the existing /api,
                                      ensuring data layer parity
```

Cloud Tasks emulator is flaky; we don't use it. Instead `internal/apps/events.go` takes a `TaskEnqueuer` interface; tests inject a fake that records enqueued tasks in a slice for assertion. Production wires up the real `cloudtasks.Client`.

### 10.3 Layer 3 — end-to-end against a fake App

Extend `tests/test_e2e.go` with a `fake-app/` mode that boots a tiny HTTP server (the "App") and walks the real connection flow:

```
1. Manifest POST → asserts 201 + valid client_id/secret/pem returned.
2. Fake App signs JWT with returned pem, calls
     POST /api/v3/app/installations/{id}/access_tokens
3. Asserts response body matches GitHub schema (jsonschema validation against
   tests/fixtures/github-openapi/installation_token.schema.json — pulled
   verbatim from GitHub's published OpenAPI spec).
4. Uses returned token to:
     a. GET /api/v3/repos/{owner}/{repo}/issues/{n}  → asserts shape vs schema.
     b. POST a comment on the issue.
5. Asserts an `issue_comment` webhook was POSTed to the fake App's receiver
   within 30 seconds, with valid X-Hub-Signature-256 (HMAC verified against
   the webhook_secret).
6. Asserts the comment appears in /api (existing endpoint) authored by
   `<slug>[bot]`.
7. Asserts that when the fake App posts another comment, GitBucket does NOT
   fan out a webhook back to it (loop prevention).
```

The schema assertions in steps 3 and 4a are the real test of GitHub compatibility. Any formatter drift fails CI before Claude/Jules ever sees the regression. Schemas live under `tests/fixtures/github-openapi/` and are refreshed quarterly from GitHub's published OpenAPI spec.

### 10.4 What's deliberately untested

- Real Claude Code or Jules end-to-end. They'd be flaky in CI and add an outbound dependency. The fake-app harness exercises the same wire protocol.
- Cloud Tasks delivery semantics under failure. We trust GCP's retry/backoff and assert only that the task was enqueued with the right target and headers.
- Cross-region behavior. MVP is single-region.

### 10.5 CI integration

Layers 1 + 2 run on every PR (the existing `go test ./internal/...`). Layer 3 runs as a separate `make e2e-apps` target invoked manually or on merge to main — same pattern as the existing `tests/test_e2e.go`.

---

## 11. Open Questions

None blocking implementation. Items below are deliberate deferrals to track in the follow-on specs:

1. **User-to-server OAuth.** Without it, Apps can only act as their `[bot]` identity, not on behalf of a specific user. Enough for "@claude on an issue" but not for actions that require user attribution.
2. **Secret rotation.** Apps can't rotate their webhook secret or private key in MVP. Adding this is straightforward (Secret Manager already supports versioning) but needs a UI surface.
3. **Per-installation event filtering UI.** MVP installs subscribe to the App's `default_events`; a user can't narrow them at install time. Schema (`installations.events`) already supports per-install overrides.
4. **GitHub Enterprise Server compatibility flag.** Some Apps detect `X-GitHub-Enterprise-Version` to choose code paths. We don't set it in MVP, which means Apps use their `github.com` defaults. Add when needed.
5. **Rate limiting headers.** GitHub returns `X-RateLimit-*` headers on every response. Some clients hard-fail without them. We add fixed headers (`X-RateLimit-Limit: 5000`, etc.) in MVP without actually rate-limiting; real rate limiting is a follow-on.

---

## 12. Migration & Rollout

No data migration required — all new collections, all new endpoints. The one mutation to existing code is replacing the hard-coded `knownSyncIdentities` map in `webhooks.go` with `apps.IsBotIdentity`; the baked-in seed list preserves current behavior, so this is functionally a no-op until a bot user is registered.

Rollout sequence:

1. Land the data model + auth plane (§4, §5) behind a feature flag. No external visibility.
2. Land the v3 REST surface (§6) and exercise via integration tests.
3. Land the outbound webhook engine (§7), wire `events.Fire` calls into existing handlers.
4. Land the manifest UI + flow (§8).
5. Connect a real Claude Code installation against a staging deploy; iterate on conformance bugs.
6. Remove the feature flag.
