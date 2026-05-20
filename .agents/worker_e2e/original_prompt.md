## 2026-05-20T15:01:30Z
Objective: Implement Milestone 8 (E2E & Frontend Validation / Git LFS).
Tasks:
1. Implement the Git LFS Batch and Transfer API in `internal/api/git.go`:
   - Register the routes (and support URLs ending with or without `.git`):
     - `POST /r/{owner}/{repo}/info/lfs/objects/batch`
     - `POST /r/{owner}/{repo}.git/info/lfs/objects/batch`
     - `PUT /r/{owner}/{repo}/info/lfs/objects/{oid}`
     - `PUT /r/{owner}/{repo}.git/info/lfs/objects/{oid}`
     - `GET /r/{owner}/{repo}/info/lfs/objects/{oid}`
     - `GET /r/{owner}/{repo}.git/info/lfs/objects/{oid}`
   - Implement `HandleLFSBatch` handler:
     - Decodes Basic Auth header and performs authorization checks (push/upload requires owner; pull/download requires owner if repository is private).
     - Decodes the JSON request body containing `operation` ("upload" or "download") and `objects` list.
     - Constructs the JSON response following the Git LFS batch API spec (with content-type `application/vnd.git-lfs+json`):
       - If `operation` is "upload", add an `upload` action with `href` pointing to `http://<host>/r/{owner}/{repo}.git/info/lfs/objects/{oid}` and propagate the Basic Auth header inside the `header` actions map.
       - If `operation` is "download", check if the object exists in GCS under path `repos/{owner}/{repo}/lfs/{oid}`. If it exists, add a `download` action pointing to the GET URL and propagate the auth header. If it does not exist, return an error/not-found status for that object.
   - Implement `HandleLFSUpload` handler (PUT):
     - Authenticates and authorizes.
     - Streams the body directly to GCS under path `repos/{owner}/{repo}/lfs/{oid}`.
   - Implement `HandleLFSDownload` handler (GET):
     - Authenticates and authorizes.
     - Streams the object from GCS under path `repos/{owner}/{repo}/lfs/{oid}` to the client.
2. Run the E2E verification test suite with LFS enabled:
   - Command: `go run tests/test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382 -lfs=true`
   - Ensure all 13 test scenarios pass successfully (including `testGitLFSPushPull`, `testCommitLogAPI`, `testFileTreeAPI`, `testBlobAPI`).
3. Verify that all package unit tests (`go test ./...`) compile and pass successfully.
4. Write a handoff report to `/Users/treycaliva/projects/gitbucket/.agents/worker_e2e/handoff.md`.
