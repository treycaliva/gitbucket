# Technical Context

## Environment Variables
- `PORT`: Server port (default 8080)
- `DEV_MODE`: Set to `true` to enable local mock authentication
- `GCS_BUCKET`: Target GCS bucket for object storage (default `git-bucket-repositories-79382`)
- `LOCAL_REPOS_ROOT`: Temporary directory for bare repository operations on the container (default `/tmp/repos`)

## Firebase Firestore Collections
- `/users`: Document ID is User UID (`uid`). Fields: `username`, `displayName`, `email`, `createdAt`.
- `/tokens`: Document contains PAT records. Fields: `uid`, `name`, `tokenHash` (SHA-256), `createdAt`, `lastUsedAt`.
- `/locks`: Document ID is `{owner}_{repo}`. Fields: `acquiredBy`, `acquiredAt`, `expiresAt`.
- `/repositories`: Document ID is `{owner}_{repo}`. Fields: `ownerUid`, `owner`, `name`, `description`, `visibility`, `defaultBranch`, `createdAt`, `updatedAt`, `branches`, `commitsCache`.

## Local Services & Ports
- Go server expected to listen on `8080`.
- React frontend runs on Vite dev server or is served statically from `frontend/dist`.

## Tools & Commands Reference
- Git smart HTTP backend CLI: `/usr/lib/git-core/git-http-backend` (or via `git --exec-path`)
- Run Go backend: `go run main.go`
- Compile frontend: `npm run build:frontend`
