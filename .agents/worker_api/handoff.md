# Handoff Report — Milestone 4 (User & PAT APIs)

## 1. Observation
- **File Paths and Changes**:
  - `main.go`:
    - Updated `getClientIP` to fix the IP spoofing vulnerability:
      ```go
      parts := strings.Split(xff, ",")
      // Google Front End (GFE) appends the actual connecting client IP to the END.
      return strings.TrimSpace(parts[len(parts)-1])
      ```
    - Registered the REST API routes on the Chi router by instantiating `api.NewAPIHandler(firestoreClient, authHandler)` and calling `apiHandler.RegisterRoutes(r)`.
    - Removed the unused `"encoding/json"` import.
  - `internal/db/db.go`:
    - Implemented Firestore-based operations:
      - `GetUidByUsername(ctx, client, username)`
      - `RegisterUsername(ctx, client, uid, username, email)` via transactional registration.
      - `GeneratePAT(ctx, client, uid, name)`
      - `VerifyPAT(ctx, client, token)` (with SHA256 hashing and updating `lastUsedAt`)
      - `ListPATs(ctx, client, uid)`
      - `RevokePAT(ctx, client, uid, tokenId)`
  - `internal/db/db_test.go`:
    - Implemented `TestGetUsernameByUID`, `TestUsernameRegistryFlow`, and `TestPATLifecycle` using the Firestore Emulator and unique, randomized suffixes to prevent state conflicts between test runs.
  - `internal/api/api.go`:
    - Implemented `APIHandler` struct and REST routes under `/api`.
    - Handled JSON inputs and formatted JSON outputs.
    - Validated user inputs via regex `^[a-zA-Z0-9-]{3,20}$`.
  - `internal/api/api_test.go`:
    - Implemented route testing covering health check, validation errors, successful registration, duplicate registration prevention, profile retrieval, PAT generation, PAT listing, and PAT revocation.
- **Verification Commands and Results**:
  - `go build ./...` completed successfully:
    ```
    The command completed successfully.
    ```
  - `FIRESTORE_EMULATOR_HOST=localhost:8084 go test -count=1 ./...` completed successfully:
    ```
    ok  	gitbucket/internal/api	0.310s
    ok  	gitbucket/internal/auth	0.486s
    ok  	gitbucket/internal/config	0.620s
    ok  	gitbucket/internal/db	1.048s
    ```
  - E2E tests (`go run tests/test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382`) succeeded for all implemented User/PAT scenarios:
    ```
    [🟢 PASS] Tier 1: Core API :: Health Endpoint Check
    [🟢 PASS] Tier 1: Core API :: User Registration
    [🟢 PASS] Tier 1: Core API :: PAT Token CRUD
    [🟢 PASS] Tier 2: Boundary/Errors :: Duplicate Username
    [🟢 PASS] Tier 2: Boundary/Errors :: Bad Repo Names
    ```

## 2. Logic Chain
- **Security Constraint**: Correcting `getClientIP` to retrieve `parts[len(parts)-1]` ensures that the client IP appended by GFE is verified instead of the client-controlled leftmost element, resolving the IP spoofing vulnerability.
- **Database Operations**: The transactional unique username check in `RegisterUsername` guarantees usernames are unique because it performs writes only if the username document in the `usernames` collection does not exist yet.
- **PAT Lifecycle**: Storing the SHA256 hashed PAT token ensures security, and looking it up dynamically in `VerifyPAT` ensures only valid, active tokens can authorize requests.
- **Testing Integrity**: Using unique suffixes (e.g., `api-uid-...`) in testing prevents state pollution, allowing tests to run cleanly and repeatedly against a persistent Firestore emulator instance.

## 3. Caveats
- E2E testing for Repository, Private Access, and Git push/pull flows are failing as they require implementing Milestones 5 and 6, which are outside the scope of Milestone 4.
- Git LFS tests fail locally due to the absence of the `git-lfs` executable on the host's system path.

## 4. Conclusion
Milestone 4 is fully completed. All database layers, API routes, and tests have been implemented genuinely, securely, and minimally. The system is ready to proceed to Milestone 5.

## 5. Verification Method
- **Command to run DB/API tests**:
  Ensure the Firestore emulator is running on `localhost:8084`, then execute:
  ```bash
  FIRESTORE_EMULATOR_HOST=localhost:8084 go test -v -count=1 ./internal/db/... ./internal/api/...
  ```
- **Command to run E2E suite**:
  Start the Firestore emulator on `localhost:8084` and start the server:
  ```bash
  DEV_MODE=true PROJECT_ID=git-bucket-79382 FIRESTORE_EMULATOR_HOST=localhost:8084 go run main.go
  ```
  Then in another shell run:
  ```bash
  go run tests/test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382
  ```
- **Expected result**: All unit tests pass, and the E2E suite output shows `PASS` for the following scenarios:
  - `Tier 1: Core API :: Health Endpoint Check`
  - `Tier 1: Core API :: User Registration`
  - `Tier 1: Core API :: PAT Token CRUD`
  - `Tier 2: Boundary/Errors :: Duplicate Username`
  - `Tier 2: Boundary/Errors :: Bad Repo Names`
