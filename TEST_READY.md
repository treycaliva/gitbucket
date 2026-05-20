# E2E Test Execution Readiness & Coverage Matrix

This document provides execution details for the E2E verification test suite and maps the individual scenarios to the platform features.

## Runner Command

The E2E test suite can be executed directly using the Go CLI runner:

```bash
# Execute suite using Firestore Emulator and target local Go server
go run tests/test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382

# Execute suite targeting a deployed Cloud Run instance and production Firestore
go run tests/test_e2e.go -url https://gitbucket-go-service-xyz.a.run.app -emulator=false -project git-bucket-79382
```

### Configurable CLI Flags
* `-url`: Target base URL of the Go server. (Default: `http://localhost:8080`)
* `-project`: Google Cloud Project ID for database operations. (Default: `git-bucket-79382`)
* `-emulator`: Controls whether the suite sets `FIRESTORE_EMULATOR_HOST=localhost:8084` before client initialization. (Default: `true`)
* `-lfs`: Enables or disables the execution of local Git LFS push/pull scenarios. (Default: `true`)

---

## Coverage Summary Matrix

The E2E test suite verifies 13 distinct scenarios across 4 conceptual tiers of testing:

| Scenario ID | Test Tier | Scenario Name | Target API / Resource | Verification Check |
|---|---|---|---|---|
| **1** | Tier 1 | Health Endpoint Check | `GET /api/health` | Validates status is `200 OK` and body payload matches `{"status": "ok"}`. |
| **2** | Tier 1 | User Registration | `POST /api/user/username` | Registers a username utilizing mock auth token headers. |
| **3** | Tier 1 | Repository Creation | `POST /api/repos` | Creates a repository with public visibility and validates metadata response. |
| **4** | Tier 1 | PAT Token CRUD | `/api/tokens` | Verifies creating, listing, and revoking Personal Access Tokens. |
| **5** | Tier 1 | Basic Auth Handshake | `/r/:owner/:repo.git/info/refs` | Uses Basic Auth (Username + PAT) to fetch Git references. |
| **6** | Tier 2 | Duplicate Username | `POST /api/user/username` | Confirms registration fails if the username is already registered. |
| **7** | Tier 2 | Bad Repo Names | `POST /api/repos` | Validates repository name constraints (length, spacing, special chars). |
| **8** | Tier 2 | Private Repo Access | `/api/repos/:owner/:repo` | Confirms private repository refs and details reject unauthorized calls. |
| **9** | Tier 3 | Git Push/Pull Flow | Git CLI over Smart HTTP | Runs local init, commit, push, and clone; inspects Firestore database refs. |
| **10** | Tier 4 | Git LFS Push/Pull | Git LFS Smart HTTP / GCS | Tracks binary files, pushes to LFS endpoint, and smudges full content on clone. |
| **11** | Tier 4 | Browse Commit Log API | `/api/repos/.../commits/:branch` | Fetches repository commit log via REST API and verifies target commit SHA. |
| **12** | Tier 4 | Browse File Tree API | `/api/repos/.../tree/:branch/*` | Verifies REST API returns the directories/files list containing pushed assets. |
| **13** | Tier 4 | Browse Blob Content API | `/api/repos/.../blob/:branch/*` | Downloads raw file blob and asserts content matches source file contents. |
