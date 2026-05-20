# Execution Plan: Go Rewrite of GitBucket Backend

This plan details the steps to replace the Node.js backend with a robust Go backend supporting Firestore metadata/locking, GCS Git object storage, and React frontend integration.

## Milestones

### 1. Initialize Go Environment & Project Setup
- **Objective**: Initialize `go.mod`, install dependencies, set up configuration.
- **Verification**: Go module initialized, basic HTTP server running and passing a health check.

### 2. Design and Build E2E Test Suite
- **Objective**: Set up programmatic E2E verification test suite (adapted from requirements) that validates repository operations, user endpoints, Git operations (push, clone, LFS), and API tree/commit browsing.
- **Verification**: Test harness runnable and outputs JSON results; publishes `TEST_READY.md`.

### 3. Implement Core HTTP Server & Router
- **Objective**: Set up router in Go, CORS middlewares, IP Gating middleware, and Firebase Auth verification.
- **Verification**: Test endpoints mock auth and respond with 200/403/401 correctly.

### 4. Implement User Profiles & PAT Authentication
- **Objective**: Port the user/token logic to Go. Store users and PAT tokens (hashed with SHA-256) in Firestore.
- **Verification**: `POST /api/user/username`, `GET /api/user/me`, and `/api/tokens` CRUD APIs are fully operational.

### 5. Implement Firestore DB & GCS Storage Clients
- **Objective**: Set up Firestore clients for metadata (repositories, locks, refs) and GCS clients for individual Git objects/packfiles/LFS assets.
- **Verification**: Unit tests verify lock acquisition, release, ref updates, and individual file uploads/downloads from GCS.

### 6. Implement Git Smart HTTP Protocol Handlers
- **Objective**: Implement `/r/:owner/:repo.git/*` endpoint. Sync refs from Firestore to local cache, download missing objects/packfiles from GCS on demand, run `git-http-backend` CGI, upload new objects/packfiles/LFS assets to GCS, and update refs in Firestore.
- **Verification**: Local git commands (clone, push, pull) succeed against the Go backend.

### 7. Implement Repository & Browse APIs
- **Objective**: Port API routes for repository management, git log (commits), git diff (show), git tree (ls-tree), and git blob (show).
- **Verification**: APIs return correct format matched to existing frontend expectations.

### 8. Frontend Integration & E2E Verification
- **Objective**: Update React frontend API endpoints configuration, run the Go server + frontend container locally, run E2E test suite, and verify all tests pass.
- **Verification**: E2E test suite completes with 100% success.
