## 2026-05-20T14:58:13Z
Objective: Implement Milestone 7 (Browse APIs).
Tasks:
1. Update `internal/api/api.go` to add and register the following REST endpoints:
   - `GET /api/repos/{owner}/{repo}/commits/{branch}`
   - `GET /api/repos/{owner}/{repo}/commit/{sha}`
   - `GET /api/repos/{owner}/{repo}/tree/{branch}`
   - `GET /api/repos/{owner}/{repo}/tree/{branch}/*`
   - `GET /api/repos/{owner}/{repo}/blob/{branch}/*`
   - `GET /api/config`
2. Implement authorization and sync helper for browse actions:
   - Helper function `authorizeGitRead(w *http.ResponseWriter, r *http.Request, owner, repo string) (*db.RepositoryMetadata, string, bool)`:
     - Fetches repository metadata from Firestore: `db.GetRepositoryMetadata(ctx, firestoreClient, owner, repo)`. If not found, returns 404.
     - Checks visibility. If private: requires authenticated user, and user's username must match `owner` (case-insensitive). If unauthorized, returns 403 or 401.
     - Syncs local cache: calls `gcs.DownloadRepo(ctx, StorageClient, BucketName, owner, repo, LocalReposRoot)`.
     - Returns repository metadata, local repo path (`<LocalReposRoot>/<owner>/<repo>.git`), and an `ok` boolean.
3. Implement `GetCommitHistory` handler:
   - Parses `limit` query param (default 50).
   - Calls the `authorizeGitRead` helper.
   - Runs `git log -n <limit> --format="%H|%an|%ae|%ad|%s" <branch>` in the local repository.
   - Parses stdout into JSON array of commits: `{sha, authorName, authorEmail, date, message}`.
   - Handles empty repo state: if no commits exist (error matches `does not have any commits` or `fatal: bad default revision`), returns `[]`.
4. Implement `GetCommitDiff` handler:
   - Calls the `authorizeGitRead` helper.
   - Runs `git show <sha>`.
   - Returns `{rawDiff: stdout}`.
5. Implement `GetTree` handler:
   - Calls the `authorizeGitRead` helper.
   - Extracts sub-path from URL (wildcard `*` in chi can be accessed via `chi.URLParam(r, "*")`).
   - Runs `git ls-tree -l --full-name <branch>:<path>` (or `<branch>` if path is empty).
   - Parses each line matching standard format: `<mode> <type> <sha> <size>\t<path>`.
   - Returns JSON array of objects: `{mode, type, sha, size, name, path}`.
   - Handles empty repo or missing path (returns `[]` on invalid object name / not exists error).
6. Implement `GetBlob` handler:
   - Calls the `authorizeGitRead` helper.
   - Extracts file path (wildcard `*`).
   - Runs `git show <branch>:<path>`.
   - Sets correct response `Content-Type` header (based on extension: `.png`/`.jpg`/`.jpeg`/`.gif`/`.webp`/`.ico` as `image/...`, `.svg` as `image/svg+xml`, `.pdf` as `application/pdf`, else `text/plain; charset=utf-8`) and writes raw file bytes.
7. Implement `GetConfig` handler:
   - Returns client configuration JSON (e.g. `devMode` based on env `DEV_MODE`, and `firebase` config variables).
8. Create/update tests in `internal/api/api_test.go` or `internal/api/browse_test.go` to verify the routing, authorization, and output of these endpoints.
9. Verify all unit tests pass cleanly with `go test ./...` and `go build ./...`.
10. Write a handoff report to `/Users/treycaliva/projects/gitbucket/.agents/worker_browse/handoff.md`.
