## 2026-05-20T14:53:19Z

Objective: Implement Milestone 6 (Git HTTP Protocol & Repository APIs).
Tasks:
1. Update `internal/api/api.go` to register and implement the Repository REST endpoints:
   - `GET /api/repos` (optional auth): lists public repositories, and if logged in, lists user's repositories (de-duplicated).
   - `POST /api/repos` (require auth): parses JSON body `{name, description, visibility}`, calls `db.CreateRepositoryMetadata(...)` with user's UID and username, returns `{success: true, owner, name}`. Validates repository name matches `^[a-zA-Z0-9-_]{3,30}$`.
   - `GET /api/repos/{owner}/{repo}` (optional auth): gets metadata. Checks visibility; if private and requester is not the owner, returns 403/401.
   - `DELETE /api/repos/{owner}/{repo}` (require auth): verifies owner, deletes GCS repo, deletes Firestore repo metadata.
2. Implement Git HTTP smart handlers in a new file `internal/api/git.go` or within `internal/api/api.go` that:
   - Mounts routes for `/r/{owner}/{repo}/*` and `/r/{owner}/{repo}.git/*`.
   - Strips the `.git` suffix from the repo path parameter.
   - Decodes Basic authentication header to verify the user using `db.VerifyPAT(ctx, firestoreClient, pat)`.
   - Performs authorization check:
     - Push/Write (`git-receive-pack` request/service query): Requires requester username to match `owner`.
     - Pull/Clone (`git-upload-pack` request/service query): If repository is private, requires requester username to match `owner`. If public, public/unauthorized access is allowed.
   - Manages local cache:
     - Defines `<LOCAL_REPOS_ROOT>/<owner>/<repo>.git` directory.
     - Compares local `last_sync_timestamp` file with `repoMeta.UpdatedAt` millisecond timestamp.
     - Acquires Firestore distributed lock (`db.AcquireLock`) if writing or if cache is outdated.
     - If cache is outdated, calls `gcs.DownloadRepo`. If it returns false (new repository), initializes a new bare Git repository:
       - Runs `git init --bare`
       - Runs `git config http.receivepack true` and `git config http.uploadpack true`
       - Runs `git symbolic-ref HEAD refs/heads/main`
     - Writes the local `last_sync_timestamp` file.
   - Executes `git-http-backend` CGI:
     - Finds the `git-http-backend` binary path (e.g. by running `git --exec-path` or checking common paths like `/usr/lib/git-core/git-http-backend`).
     - Passes standard CGI environment variables: `GIT_PROJECT_ROOT`, `GIT_HTTP_EXPORT_ALL`, `PATH_INFO`, `REQUEST_METHOD`, `QUERY_STRING`, `CONTENT_TYPE`, `REMOTE_USER`, `REMOTE_ADDR`.
     - Pipes request body to standard input, parses CGI stdout headers, sets response headers, and streams the body.
   - Implements post-push actions:
     - Compresses and uploads the repository cache back to GCS (`gcs.UploadRepo`).
     - Reads the branches list: running `git --git-dir="..." for-each-ref --format="%(refname:short)" refs/heads/` and updates the `branches` list in Firestore repository metadata.
     - Syncs local timestamp file after metadata update.
     - Releases distributed lock in a `defer` block.
3. Update `main.go` to mount these new Git HTTP routes under Chi router.
4. Add unit tests in `internal/api/api_test.go` or `internal/api/git_test.go` verifying the routing, repository creation, deletion, list, and the git info/refs endpoint.
5. Run `go test ./...` and `go build ./...` to verify everything compiles and passes tests cleanly.
6. Write a handoff report to `/Users/treycaliva/projects/gitbucket/.agents/worker_git/handoff.md`.
