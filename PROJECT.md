# Project: GitBucket Go Backend Rewrite

## Architecture
- **Backend Language**: Go (Golang)
- **Framework**: `net/http` and standard libraries (optionally `github.com/go-chi/chi/v5` for routing)
- **Database**: Cloud Firestore for users, PATs, repositories, locks, and refs (multi-region ACID consistency)
- **Storage**: Google Cloud Storage (GCS) for loose Git objects, pack-files, and Git LFS assets
- **Git Smart HTTP**: Custom CGI wrapper around `git-http-backend` that loads/stores objects dynamically from GCS and syncs refs to/from Firestore.

## Code Layout
- `go.mod`, `go.sum` — Go dependency management files
- `main.go` — Web server entry point, middleware integration
- `internal/config/config.go` — Port, GCS Bucket, Dev Mode settings
- `internal/auth/auth.go` — Firebase ID Token and PAT authorization middleware
- `internal/db/db.go` — Firestore queries: locking, user registry, repositories, tokens, refs
- `internal/storage/storage.go` — GCS operations for individual loose objects, packfiles, and LFS assets
- `internal/git/git.go` — Git Smart HTTP CGI execution and GCS object sync
- `internal/api/api.go` — Repository browser REST endpoints (commits, diffs, tree structure, blobs)

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | Initialize Environment | Go module init, Dockerfile update, package skeleton | None | DONE |
| 2 | Setup E2E Test Suite | Design and build multi-tier E2E tests, TEST_READY.md | M1 | DONE |
| 3 | Core Server & Router | Web server, IP gating, CORS, Firebase auth middleware | M1 | DONE |
| 4 | User & PAT APIs | Register username, get profile, generate/list/revoke PATs | M3 | DONE |
| 5 | Firestore & GCS Clients | Implement DB locks/metadata & GCS asset handlers | M1 | DONE |
| 6 | Git HTTP Protocol | Smart HTTP operations, on-demand object fetch, Firestore ref sync | M4, M5 | DONE |
| 7 | Browse APIs | Commits log, show diff, ls-tree, show blob Go endpoints | M5, M6 | DONE |
| 8 | E2E & Frontend Validation | Final integration, verify 100% test pass on Go server | M2, M6, M7 | DONE |

## Interface Contracts

### Authenticated Handshake (Web Frontend)
- HTTP Header: `Authorization: Bearer mock_<uid>` (Dev mode) or `Authorization: Bearer <Firebase_ID_Token>` (Production)

### Git CLI Authentication
- HTTP Basic Authentication: Username = GitBucket username, Password = PAT token (`gb_pat_...`)

### REST Endpoints
- `POST /api/user/username` -> Request: `{"username": "name"}` -> Response: `{"success": true, "username": "name"}`
- `GET /api/user/me` -> Response: `{"uid": "...", "email": "...", "username": "..."}`
- `GET /api/tokens` -> Response: `[{"id": "...", "name": "...", "createdAt": "...", "lastUsedAt": "..."}]`
- `POST /api/tokens` -> Request: `{"name": "label"}` -> Response: `{"token": "gb_pat_...", "name": "label"}`
- `DELETE /api/tokens/:id` -> Response: `{"success": true}`
- `GET /api/repos` -> Response: list of repository metadata documents
- `POST /api/repos` -> Request: `{"name": "repo-name", "description": "desc", "visibility": "public|private"}` -> Response: `{"success": true, "owner": "username", "name": "repo-name"}`
- `GET /api/repos/:owner/:repo` -> Response: Repository metadata
- `DELETE /api/repos/:owner/:repo` -> Response: `{"success": true}`
- `GET /api/repos/:owner/:repo/commits/:branch` -> Response: `[{"sha": "...", "authorName": "...", "authorEmail": "...", "date": "...", "message": "..."}]`
- `GET /api/repos/:owner/:repo/commit/:sha` -> Response: `{"rawDiff": "..."}`
- `GET /api/repos/:owner/:repo/tree/:branch/*` -> Response: list of tree items: `{"mode": "...", "type": "...", "sha": "...", "size": 0, "name": "...", "path": "..."}`
- `GET /api/repos/:owner/:repo/blob/:branch/*` -> Response: raw binary file contents
- `GET /api/config` -> Response: Firebase configuration details & DevMode flag

#### Collaborators
- `GET /api/repos/:owner/:repo/collaborators` (OptionalWebAuth; requires read access)
  -> Response: `[{"uid": "...", "username": "...", "addedAt": "...", "addedBy": "..."}]` (empty array if none).
  Status codes: 200, 403 (no read access), 404 (repo not found), 500.
  Lists non-owner users with explicit access to the repo. The owner is implicit and not included.
- `POST /api/repos/:owner/:repo/collaborators` (RequireWebAuth; owner only)
  -> Request: `{"username": "name"}` (must match `^[a-zA-Z0-9-]{3,20}$`)
  -> Response: 204 No Content (empty body).
  Status codes: 204, 400 (bad/missing username, or target is the owner), 401, 403 (not owner), 404 (repo or user not found), 500.
- `DELETE /api/repos/:owner/:repo/collaborators/:username` (RequireWebAuth; owner only)
  -> Response: 204 No Content (no-op if username is not present).
  Status codes: 204, 401, 403 (not owner), 404 (repo not found), 500.

#### Branch Protection
Rule object shape (used in list/create/update responses and create/update requests):
```
{
  "id": "...",                          // assigned on create; required in PUT path
  "pattern": "main|release/*|...",      // filepath.Match semantics, 1–200 chars
  "pushAllowlist": ["uid1", ...],       // UIDs allowed to push directly; empty = nobody (PR-only)
  "mergeAllowlist": ["uid1", ...],      // UIDs allowed to merge PRs into matched branches
  "requirePullRequest": true,
  "requireApprovals": 0,
  "requireCodeownerApproval": false,
  "blockForcePush": true,
  "blockDeletion": true
}
```
- `GET /api/repos/:owner/:repo/branch-protection` (OptionalWebAuth; requires read access)
  -> Response: array of Rule objects (empty array if none).
  Status codes: 200, 403, 404, 500.
- `POST /api/repos/:owner/:repo/branch-protection` (RequireWebAuth; owner only)
  -> Request: Rule object (id is ignored / assigned server-side)
  -> Response: 201 Created with the persisted Rule including assigned `id`.
  Status codes: 201, 400 (invalid pattern: empty, >200 chars, or malformed glob), 401, 403, 404, 500.
- `PUT /api/repos/:owner/:repo/branch-protection/:ruleId` (RequireWebAuth; owner only)
  -> Request: full Rule object (full replace)
  -> Response: 204 No Content.
  Status codes: 204, 400 (invalid pattern), 401, 403, 404 (repo or rule not found), 500.
- `DELETE /api/repos/:owner/:repo/branch-protection/:ruleId` (RequireWebAuth; owner only)
  -> Response: 204 No Content.
  Status codes: 204, 401, 403, 404, 500.

#### CODEOWNERS
- `GET /api/repos/:owner/:repo/codeowners?path=:dir&ref=:ref` (OptionalWebAuth; same read semantics as `tree`/`blob`)
  -> Response: `{"entries": {"<childName>": ["@owner1", "@team/x", ...], ...}}`.
  Keys are immediate children of `path` (default: repo root) at `ref` (default: repo `defaultBranch`, falling back to `main`). Entries with no matching CODEOWNERS rule are omitted; the map may be empty.
  Status codes: 200 (also returned with empty `entries` for empty repo / unknown ref / missing dir / parse errors), 403, 404, 500 (other git errors).
  Resolves CODEOWNERS from `CODEOWNERS`, `.gitbucket/CODEOWNERS`, or `docs/CODEOWNERS` at the requested ref.

#### Pull Request Reviews
- `POST /api/repos/:owner/:repo/pulls/:number/reviews` (RequireWebAuth; requires read access; PR author may not review their own PR)
  -> Request: `{"state": "approved|changes_requested|commented", "body": "..."}`
  -> Response: 204 No Content. Re-submission by the same user replaces the prior review (one review per (PR, user)).
  Status codes: 204, 400 (bad number, bad body, or invalid state), 401, 403 (no read access, or self-review), 404 (repo or PR not found), 500.
- `GET /api/repos/:owner/:repo/pulls/:number/reviews` (OptionalWebAuth; requires read access)
  -> Response: `[{"uid": "...", "username": "...", "state": "approved|changes_requested|commented", "body": "...", "submittedAt": "..."}]`.
  Status codes: 200, 403, 404, 500.

#### Pull Request Response Shape Change
The `PullRequest` object returned by the `pulls` endpoints (`GET /api/repos/:owner/:repo/pulls`, `GET .../pulls/:number`, etc.) now includes:
- `requestedReviewers: string[]` — usernames auto-resolved from CODEOWNERS at PR open time against the PR's diff. May be empty if no CODEOWNERS rules matched. Computed once at creation; not recomputed when the PR is updated.

### Git Smart HTTP — Branch-Protection Enforcement at Push Time
Push-time enforcement runs against `git-receive-pack` results **after** the CGI has already streamed its response, so a rejected push **does not return HTTP 403**. The HTTP response indicates the CGI accepted the pack; rejection is enforced by:

1. Diffing local refs (pre vs. post CGI execution) to derive the proposed `RefUpdate` set.
2. Running `git.EnforcePush(rules, updates, pusherUid)` against the rules stored in Firestore.
3. If any updates are rejected: **skipping GCS upload and Firestore ref sync-back**, and deleting the local `last_sync_timestamp` file.

Effect: from the client's perspective the push appears to succeed at the HTTP layer, but the rejected refs never become canonical. The next request that materializes the repo will pull the unchanged refs from GCS/Firestore, so to any other clone the push is invisible. Rejection reasons are logged server-side (`[Git HTTP] REJECTED ref=... reason=...`).

## Storage Operations

### GCS object versioning

**Decision: not enabled.** Git objects (loose, pack, LFS blobs) are content-addressed
by SHA — they are immutable. Versioning protects against overwrite/delete of mutable
objects and provides no recovery benefit on content-addressed storage. The only
mutable state that benefits from recovery — refs and metadata — lives in Firestore.

### GCS soft-delete

The repositories bucket has soft-delete enabled with 7-day retention to guard against
accidental bulk-deletion or GC bugs. Enable on a new environment with
`scripts/enable-soft-delete.sh` (requires `gcloud` auth and `GCS_BUCKET` env).
