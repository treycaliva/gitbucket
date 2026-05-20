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
