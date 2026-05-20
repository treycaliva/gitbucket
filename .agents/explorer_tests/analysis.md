# E2E Test Suite Research & Design Analysis

## Summary
A comprehensive investigation was conducted to identify any historical E2E test files in Git history, concluding that no Git repository is currently initialized in the workspace, meaning Git history is unavailable. To satisfy the verification requirements, a multi-tier Go E2E test suite was designed and drafted as `proposed_test_e2e.go`, covering REST APIs, authentication boundary conditions, transactional Git operations via CLI execution, Firestore state check validation, and Git LFS large asset push/pull operations.

---

## 1. Research Findings: Git History & Deleted Test Files
* **Git Repository Status**: Running `git status` or `git log` anywhere inside the `/Users/treycaliva/projects/gitbucket` directory tree or its parent directories returns:
  ```
  fatal: not a git repository (or any of the parent directories): .git
  ```
* **Implication**: There is no Git database (`.git` folder) present in the active workspace. Consequently, it is impossible to retrieve any deleted file (like `test_e2e.js`) from Git history.
* **Alternative Resource Alignment**: To build a matching E2E verification workflow, the legacy Express Node.js routes in `server/api.js` and `server/git-http.js` were thoroughly reviewed to document the exact API contracts, path structures, header schemas, response payloads, and authentication mechanics.

---

## 2. E2E Test Suite Architecture & Design
The designed test suite is written in Go to execute as a programmatic tool (e.g. `go run tests/test_e2e.go`). It uses the standard library HTTP client to hit REST and Git CGI endpoints, and uses local CLI subprocesses (`git` and `git-lfs`) to simulate real developer pushes/pulls. 

### Multi-Tiered Verification Structure
| Tier | Focus | Target Operations |
|---|---|---|
| **Tier 1** | Core API Coverage | `/api/health`, Mock user registration `/api/user/username`, `/api/repos` creation, PAT token CRUD lifecycle, HTTP Basic auth handshake |
| **Tier 2** | Boundary & Error Handling | Duplicate username collision rejection, unauthorized access/clone block for private repositories, invalid repository name format validation |
| **Tier 3** | Cross-Feature combinations | Local repository initialization -> Mock PAT token authentication -> Git CLI Push -> Git CLI Clone & Pull -> Firestore reference database SHA lookup |
| **Tier 4** | Real-world & LFS | Large binary file generation (~5MB) -> Git LFS tracking & push -> Smudge-enabled Git Clone pull -> Commit log history REST API -> Directory tree browser -> Blob reader |

---

## 3. Go E2E Test Suite Implementation Design (`proposed_test_e2e.go`)
A programmatic script has been drafted at `/Users/treycaliva/projects/gitbucket/.agents/explorer_tests/proposed_test_e2e.go`. 

### Key Design Decisions
1. **Developer Mock Auth Mode Compatibility**: The test suite targets the custom mock token authorization `Bearer mock_<uid>` defined in the backend routing to securely register usernames and repositories without relying on third-party OAuth setups during local execution.
2. **Git CGI Integration via System Subprocesses**: Rather than using lightweight mock libraries, the test suite executes actual local `git` and `git-lfs` commands against the Go server port. This exercises:
   - Git Basic Authentication (`http://username:pat@host/r/owner/repo.git`)
   - CGI protocol routing (`git-upload-pack`, `git-receive-pack`)
   - Google Cloud Storage (GCS) raw object and LFS asset upload/downloads
3. **Programmatic Firestore Ref Checking**: The test suite utilizes the official `cloud.google.com/go/firestore` client to inspect the Firestore database. It connects to the emulator when `FIRESTORE_EMULATOR_HOST` is set, confirming that branches and reference SHAs match the local push.
4. **Git LFS Validation via Smudging Verification**: The LFS push is verified not just by completing successfully, but by cloning the repository to a clean path with `GIT_LFS_SKIP_SMUDGE=0` and verifying that the final file size matches the 5MB original file exactly (rather than the short text pointer file).

---

## 4. Execution & Verification Flow
Once the Go backend is implemented, the E2E verification test suite can be run directly using the standard Go compiler.

### Command Execution
```bash
# To run against local server on default port using Firestore Emulator
go run .agents/explorer_tests/proposed_test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382

# To run against a deployed Cloud Run service and real Firestore instance
go run .agents/explorer_tests/proposed_test_e2e.go -url https://gitbucket-service-xyz-uc.a.run.app -emulator=false -project git-bucket-79382
```

### Exit Codes & Reporting
* **Success (Exit 0)**: Prints a detailed console test matrix displaying `[PASS]` for all 13 test scenarios.
* **Failure (Exit 1)**: Outputs `[FAIL]` detailing the exact step, HTTP status code, or command error message that triggered the failure, and exits with non-zero status to halt CI pipelines.
