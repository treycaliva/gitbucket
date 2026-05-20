## 2026-05-20T14:42:35Z
Objective: Implement Milestone 3 (Core Server & Router).
Tasks:
1. Update `internal/config/config.go` to load configuration from environment variables: `PORT`, `GCS_BUCKET`, `DEV_MODE`, `RESTRICTED_IP`, `PROJECT_ID` / `GOOGLE_CLOUD_PROJECT`.
2. Update `internal/db/db.go` to initialize the Firestore Client and implement the `GetUsernameByUID(ctx, client, uid)` function to fetch usernames from the `users` Firestore collection.
3. Update `internal/auth/auth.go` to implement `RequireWebAuth` and `OptionalWebAuth` HTTP middlewares using the `firebase.google.com/go/v4` auth package and our dev-mode mock token handler.
4. Update `main.go` to set up the Chi Router, register standard logging/recovery middlewares, implement the IP Gating and CORS middlewares, serve static frontend assets with SPA fallback, and handle `/api/health`.
5. Run `go build ./...` and `go vet ./...` to verify that everything compiles cleanly and complies with Go syntax guidelines.
6. Write a handoff report to `/Users/treycaliva/projects/gitbucket/.agents/worker_server/handoff.md`.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A Forensic Auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Working directory for coordination metadata: `/Users/treycaliva/projects/gitbucket/.agents/worker_server/`
