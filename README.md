# GitBucket

A serverless, GitHub-style Git host built on Google Cloud. GitBucket runs as a single Go binary on Cloud Run, stores Git objects in GCS, and uses Firestore for metadata, refs, and pull requests — no persistent disks, no VMs, no managed Git server.

## Features

- **Git Smart HTTP** — `git clone`, `push`, `pull`, `fetch` over HTTPS, backed by `git-http-backend` with on-demand object materialization from GCS.
- **Git LFS** — large-file storage backed by the same GCS bucket.
- **Personal access tokens** — `gb_pat_…` tokens for CLI auth; Firebase ID tokens for the web UI.
- **Repository browser** — commits, diffs, trees, blobs, branches, and tags via REST.
- **Pull requests** — open, review, merge, with branch protection rules, push/merge allowlists, required approvals, and CODEOWNERS support.
- **Collaborators** — per-repo access control on top of public/private visibility.
- **GitHub sync** — mirror repositories in/out of GitHub.
- **React SPA frontend** — served by the same Go binary in production.

## Architecture

```
┌──────────────┐       ┌─────────────────────────────────────────┐
│  Browser SPA │──HTTP─▶│            Cloud Run (Go)              │
└──────────────┘        │  chi router                            │
┌──────────────┐  Git   │  ├─ /api/*    REST (auth, repos, PRs)  │
│   git CLI    │──HTTP─▶│  ├─ /r/{o}/{r}[.git]/*  Smart HTTP+LFS │
└──────────────┘        │  └─ /*        SPA fallback             │
                        └──────┬────────────────────┬────────────┘
                               │                    │
                       ┌───────▼────────┐   ┌───────▼────────┐
                       │   Firestore    │   │      GCS       │
                       │ users, PATs,   │   │ loose objects, │
                       │ repos, refs,   │   │ pack-files,    │
                       │ PRs, locks     │   │ LFS blobs      │
                       └────────────────┘   └────────────────┘
```

There is no persistent local disk. `LOCAL_REPOS_ROOT` (default `/tmp/repos`) is a scratch directory: each Git request materializes the repo from GCS, runs `git-http-backend`, then syncs new objects back to GCS and refs back to Firestore. See `PROJECT.md` for the full design.

## Quickstart

### Prerequisites

- Go 1.21+
- Node 20+ (for the frontend)
- `git`, `git-lfs`
- A Google Cloud project with Firestore + a GCS bucket, **or** the Firestore + GCS emulators for local dev

### Run locally (dev mode)

```bash
# 1. Build the frontend (the Go server serves frontend/dist)
cd frontend && npm install && npm run build && cd ..

# 2. Configure env (see Environment below). For local dev:
export DEV_MODE=true
export PROJECT_ID=git-bucket-79382
export GCS_BUCKET=git-bucket-repositories-79382
export FIRESTORE_EMULATOR_HOST=localhost:8084
export STORAGE_EMULATOR_HOST=http://localhost:9023

# 3. Run the server
go run main.go
# → http://localhost:8080
```

In `DEV_MODE=true`, the web UI accepts `Authorization: Bearer mock_<uid>` instead of real Firebase ID tokens, which makes browser testing and the E2E suite work without auth setup.

### Frontend dev server

```bash
cd frontend
npm run dev   # Vite dev server with HMR, proxies /api to :8080
```

### Tests

```bash
go test ./internal/...                  # unit tests
go test ./internal/api -run TestName    # single test

# End-to-end (needs Firestore emulator on :8084)
export FIRESTORE_EMULATOR_HOST=localhost:8084
go run tests/test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382
```

See `TEST_READY.md` for the 13-scenario E2E matrix and `TEST_INFRA.md` for emulator setup.

### Docker

```bash
docker build -t gitbucket .
docker run -p 8080:8080 --env-file .env gitbucket
```

The multi-stage build compiles the React SPA and the Go binary into a single image.

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `PROJECT_ID` / `GOOGLE_CLOUD_PROJECT` | `git-bucket-79382` | GCP project for Firestore + Firebase Admin |
| `GCS_BUCKET` | — | Bucket holding Git objects and LFS blobs |
| `LOCAL_REPOS_ROOT` | `/tmp/repos` | Scratch dir for `git-http-backend` working copies |
| `LOCAL_REPOS_MAX_BYTES` | half the cgroup memory limit (else 2 GiB) | Disk budget for materialized repos; LRU-evicted above it. `0` disables eviction |
| `DEV_MODE` | `false` | Enables `mock_<uid>` bearer tokens for the web UI |
| `RESTRICTED_IP` | — | Comma-separated allowlist; loopback always passes |
| `STORAGE_EMULATOR_HOST` | — | When set, GCS client runs unauthenticated against the emulator |
| `FIRESTORE_EMULATOR_HOST` | — | When set, Firestore client targets the emulator |

The committed `.env` is the local dev config. Production secrets live in Cloud Run environment configuration, not in the repo.

## Authentication

- **Web (browser)**: `Authorization: Bearer <Firebase ID token>` (prod) or `Bearer mock_<uid>` (dev).
- **Git CLI**: HTTP Basic Auth — username = GitBucket username, password = a PAT (`gb_pat_…`) created at `/settings/tokens`.

```bash
git clone https://USERNAME:gb_pat_xxx@gitbucket.example.com/r/USERNAME/repo.git
```

## REST API

The full contract lives in [`PROJECT.md` § Interface Contracts](./PROJECT.md). High-level surface:

- `POST /api/user/username`, `GET /api/user/me` — identity
- `GET|POST|DELETE /api/tokens[...]` — PATs
- `GET|POST /api/repos`, `GET|DELETE /api/repos/:owner/:repo` — repository CRUD
- `GET /api/repos/:owner/:repo/commits/:branch`, `/commit/:sha`, `/tree/:branch/*`, `/blob/:branch/*` — browse
- `/api/repos/:owner/:repo/collaborators[...]` — access control
- `/api/repos/:owner/:repo/pulls[...]` — pull requests, reviews, merges
- `/api/repos/:owner/:repo/branch-protection[...]` — protection rules
- `GET /api/config` — Firebase config + `DEV_MODE` flag for the SPA

## Project layout

```
main.go                  HTTP server, chi router, SPA fallback
internal/
  config/                Env-driven configuration
  auth/                  Firebase ID token + PAT middleware
  db/                    Firestore: users, PATs, repos, refs, PRs, locks
  storage/               GCS: loose objects, pack-files, LFS
  api/
    api.go               REST handlers
    git.go               Smart HTTP CGI + GCS/Firestore sync
    pull_requests.go     PR endpoints
frontend/                React SPA (Vite). Build output → frontend/dist
tests/test_e2e.go        End-to-end test runner
docs/                    Additional design notes
PROJECT.md               Source of truth for the REST contract
CLAUDE.md                Conventions and gotchas
TEST_READY.md            E2E scenario matrix
```

> `server/` is a **legacy Node/Express implementation** kept only for reference. It is not built or deployed. The root `package.json` is also legacy — the only live JS build is `frontend/`.

## Deployment

GitBucket targets Cloud Run. A typical deploy:

```bash
gcloud run deploy gitbucket \
  --source . \
  --region us-central1 \
  --allow-unauthenticated \
  --set-env-vars PROJECT_ID=…,GCS_BUCKET=…
```

The bucket should have **object versioning disabled** (Git objects are content-addressed) and soft-delete enabled for recovery. See `PROJECT.md` § GCS for rationale.

## Contributing

- Username regex: `^[a-zA-Z0-9-]{3,20}$`
- Repo name regex: `^[a-zA-Z0-9-_]{3,30}$`

Keep frontend and backend validation in sync if you change either. The REST contract in `PROJECT.md` is the source of truth for response shapes the SPA depends on — update it alongside any handler change.
