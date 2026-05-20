## 2026-05-20T14:35:05Z

Design and build a serverless Git platform (GitBucket) using a Go (Golang) backend, stateless Cloud Run for compute, Firestore for metadata/locking, and Google Cloud Storage (GCS) for raw Git object, pack-file, and LFS asset storage, mapping to the existing React frontend.

Working directory: /Users/treycaliva/projects/gitbucket
Integrity mode: development

## Requirements

### R1. Go Backend & API Layer
Rewrite the backend server entirely in Go (Golang), exposing REST APIs for repository navigation (files tree, commits list, diffs, config) and handling Git Smart HTTP protocols (`git-upload-pack`, `git-receive-pack`).

### R2. Native GCS Git Storage
Store raw Git loose objects (commits, trees, blobs), pack-files, and Git LFS assets directly in Google Cloud Storage. Avoid monolithic tarball syncs by parsing and fetching packfiles/objects dynamically from GCS.

### R3. Firestore State & Locks
Store Git references (branches/tags mapping to SHAs), user profiles, repository visibility, and transaction lock leases in Firestore to ensure multi-region ACID consistency.

### R4. React Frontend Integration
Connect the existing Vite-based React SPA frontend to the Go backend, ensuring full compatibility with existing UI views (Login, Dashboard, Repositories, Commits, Personal Access Tokens).

## Acceptance Criteria

### Git Operations
- Git push of new commits, branches, and tags successfully updates GCS objects and Firestore references.
- Git clone/pull retrieves the exact repository state pushed.
- Git LFS pushes and pulls large file assets securely.

### API & Frontend
- Files tree explorer, commits log, and line-by-line diff viewing render accurately using Go APIs.
- User authentication with Firebase Auth/PAT works end-to-end.

### Verification
- Programmatic E2E verification test suite (similar to `test_e2e.js` but updated for Go) passes.
