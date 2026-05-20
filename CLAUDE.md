# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Big picture

GitBucket is a serverless Git host (think mini-GitHub) targeting Cloud Run. The active backend is **Go** (`main.go` + `internal/`); the `server/` directory is a **legacy Node/Express implementation** kept for reference and is not built or deployed — do not modify it expecting it to run. The Node `package.json` at the repo root is also legacy; the only live JS build is the React SPA under `frontend/`.

Storage model:
- **Firestore** holds users, PATs, repo metadata, refs (branch/tag → SHA), pull requests, and transaction locks. Default project `git-bucket-79382`.
- **GCS** holds raw Git loose objects, pack-files, and LFS blobs. Default bucket `git-bucket-repositories-79382`.
- There is **no persistent local disk** for repos. `LOCAL_REPOS_ROOT` (default `/tmp/repos`) is a working directory: `internal/api/git.go` materializes a repo on demand by pulling objects from GCS, runs `git-http-backend` (CGI) against it, then syncs new objects back to GCS and refs back to Firestore. The `last_sync_timestamp` file in each local repo gates re-sync. Any code touching a local working copy must coordinate with this sync cycle.

Routing (chi, set up in `main.go`):
- `/api/*` — REST (handlers in `internal/api/api.go`, browse helpers split into `git.go` and `pull_requests.go`).
- `/r/{owner}/{repo}[.git]/*` — Git Smart HTTP and LFS endpoints. These bypass the SPA fallback.
- Everything else falls through to `frontend/dist/index.html` (SPA). The fallback in `main.go` explicitly excludes `/api/` and `/r/` — preserve that guard when editing.

Auth has two modes, both handled by `internal/auth/auth.go`:
- **Web (browser)**: `Authorization: Bearer <Firebase ID token>` in prod, or `Bearer mock_<uid>` when `DEV_MODE=true`. `RequireWebAuth` vs `OptionalWebAuth` middleware groups in `api.go` determine which endpoints require login.
- **Git CLI**: HTTP Basic Auth with username + PAT (`gb_pat_...`). Verified inside the Git HTTP handler against Firestore.

## Common commands

Backend (run from repo root):
```bash
go build -o gitbucket main.go           # build
go run main.go                          # run (reads .env via shell, not auto-loaded)
go test ./internal/...                  # all Go unit tests
go test ./internal/api -run TestName    # single test
```

Frontend (from `frontend/`):
```bash
npm run dev        # Vite dev server
npm run build      # outputs to frontend/dist — required for Go server to serve UI
npm run lint
```

E2E (requires Go, `git`, `git-lfs`, and a Firestore emulator on `:8084`):
```bash
export FIRESTORE_EMULATOR_HOST=localhost:8084
go run tests/test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382
# Flags: -url, -project, -emulator, -lfs. See TEST_READY.md for the 13-scenario matrix.
```

Docker (multi-stage, builds frontend then Go binary):
```bash
docker build -t gitbucket .
docker run -p 8080:8080 --env-file .env gitbucket
```

## Environment

Loaded in `internal/config/config.go` from env vars (no dotenv in Go — export them or use `--env-file`):
- `PORT` (default 8080), `GCS_BUCKET`, `PROJECT_ID` / `GOOGLE_CLOUD_PROJECT` (default `git-bucket-79382`), `DEV_MODE`, `RESTRICTED_IP` (comma-separated allowlist; loopback always passes), `LOCAL_REPOS_ROOT` (default `/tmp/repos`), `STORAGE_EMULATOR_HOST` (when set, GCS client runs unauthenticated against the emulator).

The committed `.env` is the dev config; production secrets live in Cloud Run env, not in the repo.

## Conventions worth knowing

- Username regex `^[a-zA-Z0-9-]{3,20}$`; repo name regex `^[a-zA-Z0-9-_]{3,30}$` (`internal/api/api.go`). Validation errors return 400 — keep both backend and frontend in sync if you change them.
- The REST contract is enumerated in `PROJECT.md` §Interface Contracts; treat that document as the source of truth for response shapes the frontend expects.
- `frontend/lint_output.txt` is a transient artifact and should not be committed (it currently is, untracked).
- GCS object versioning is intentionally disabled — Git objects are content-addressed. See `PROJECT.md` §GCS. Soft-delete (7d) is the recovery mechanism.
