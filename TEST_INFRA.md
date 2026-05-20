# Test Infrastructure and E2E Verification Setup

This document details the design, architecture, setup requirements, and coverage thresholds for the GitBucket E2E verification test suite.

## E2E Test Setup

To execute the E2E test suite locally, the following environment and dependencies are required:

### Dependencies
1. **Go Toolchain**: Go 1.18 or higher is required to compile and run the E2E test suite.
2. **Git CLI**: The standard Git CLI tool must be installed and available on the system path to simulate developer interactions.
3. **Git LFS CLI**: Git LFS must be installed and configured on the system path.
4. **Firestore Emulator**: A running Firestore Emulator instance. By default, the emulator is expected on `localhost:8084`.

### Environment Configuration
The test runner configures the Firestore environment dynamically. If using the emulator, make sure `FIRESTORE_EMULATOR_HOST` is set:
```bash
export FIRESTORE_EMULATOR_HOST=localhost:8084
```

---

## Feature Inventory

The E2E test suite is mapped directly to the core features and api paths of the GitBucket backend platform:

* **Health Monitoring**: Verification of server availability and readiness at `/api/health`.
* **User Accounts**: Registration of user profiles and prevention of duplicate usernames.
* **Personal Access Tokens (PAT)**: Full CRUD lifecycle of Personal Access Tokens used for Git CLI operations.
* **Repository Management**: Validation of repository creation, name formats, and public/private accessibility levels.
* **Git Smart HTTP Protocol**: Execution of Basic Auth handshake and Smart HTTP operations via Git CLI commands.
* **ACID Reference Storage**: Post-push Firestore reference consistency verification for branch and SHA tracking.
* **Git LFS Storage**: Large binary tracking, uploading, smudging, and download verification.
* **Repository Browsing REST APIs**: Browse commits log, file tree structure, and individual file blobs on Git branches.

---

## Test Architecture

The E2E test suite is constructed as a multi-tiered testing utility written in Go (`tests/test_e2e.go`).

```
                +-----------------------------------------+
                |           Go E2E Test Runner            |
                |          (tests/test_e2e.go)            |
                +----+-------------------------------+----+
                     |                               |
        (HTTP REST / Git Handshake)            (CLI Subprocesses)
                     |                               |
                     v                               v
         +-----------+-----------+        +----------+----------+
         |     Go HTTP Server    |        |  git / git-lfs CLI  |
         |      (Port 8080)      |        +----------+----------+
         +-----------+-----------+                   |
                     |                               v
                     |                    +----------+----------+
                     |                    |    Local Git Temp   |
                     |                    |     Repositories    |
                     |                    +---------------------+
                     v
         +-----------+-----------+
         |   Firestore Emulator  |
         |      (Port 8084)      |
         +-----------------------+
```

### Flow execution mechanics:
1. **Direct HTTP clients**: Leverages Go's native `net/http` to hit REST endpoints and inspect returned JSON schemas.
2. **Local CLI Subprocesses**: Spawns local OS subprocesses (`exec.Command`) to run actual `git` and `git-lfs` commands against the target server URL.
3. **Clean Sandboxed Repositories**: Programmatically initializes and tears down temporary local folders to perform pushes, clones, and smudging checks.
4. **Direct Firestore Inspection**: Uses the official Firestore SDK client to query the database and assert that backend states align with client push events.

---

## Coverage Thresholds

The following coverage thresholds are established to verify completion of any backend rewrite:

| Metric | Target Threshold | Validation Mechanism |
|---|---|---|
| **E2E Scenario Pass Rate** | 100% (13/13 scenarios) | Test runner console output and exit code status |
| **Core REST Coverage** | 100% of defined routes | Health, User registry, Repository, and PAT token endpoints |
| **Git Protocol Handshake** | 100% compliance | Successful basic auth authentication against HTTP Git endpoints |
| **Git LFS Completeness** | 100% asset smudging | Cloned LFS binary matches original payload size (no pointer smudging failure) |
| **Firestore State Verification** | 100% sync consistency | Branch list and ref SHAs in Firestore match local Git state |
