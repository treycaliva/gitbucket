# BRIEFING — 2026-05-20T15:00:45Z

## Mission
Implement Milestone 7 (Browse APIs) for gitbucket, including REST endpoints for repository commits, commit diff, file tree, file blob, and configuration, along with authorization, caching, and comprehensive tests.

## 🔒 My Identity
- Archetype: implementer, qa, specialist
- Roles: implementer, qa, specialist
- Working directory: /Users/treycaliva/projects/gitbucket/.agents/worker_browse/
- Original parent: 206cf57f-33e1-4857-b366-a8f733909d3b
- Milestone: Milestone 7 (Browse APIs)

## 🔒 Key Constraints
- CODE_ONLY network mode: No external network access.
- Only write to my working directory `/Users/treycaliva/projects/gitbucket/.agents/worker_browse/` for agent files (e.g. BRIEFING.md, progress.md, handoff.md).
- Follow Go layout guidelines (source in designated dirs, tests co-located, etc.).
- Follow the Integrity Mandate: no cheats, no mock test results, no dummy implementations.

## Current Parent
- Conversation ID: 206cf57f-33e1-4857-b366-a8f733909d3b
- Updated: not yet

## Task Summary
- **What to build**: Add browse-related REST endpoints in `internal/api/api.go` and implement the logic to run git commands locally on sync'd GCS repositories, plus configuration endpoint.
- **Success criteria**: All new routes compile, authenticate properly, cache via GCS, return structured/raw git data, and pass tests cleanly.
- **Interface contracts**: Endpoints matching:
  - `GET /api/repos/{owner}/{repo}/commits/{branch}`
  - `GET /api/repos/{owner}/{repo}/commit/{sha}`
  - `GET /api/repos/{owner}/{repo}/tree/{branch}`
  - `GET /api/repos/{owner}/{repo}/tree/{branch}/*`
  - `GET /api/repos/{owner}/{repo}/blob/{branch}/*`
  - `GET /api/config`
- **Code layout**: Go code is in `internal/` and tests are co-located or in same package.

## Change Tracker
- **Files modified**:
  - `internal/db/db.go`: Added `RepositoryMetadata` struct and conversion helper.
  - `internal/api/api.go`: Registered endpoints, added imports, and implemented handlers/helpers.
  - `internal/api/browse_test.go`: Added full test coverage for the browse REST endpoints.
- **Build status**: Pass
- **Pending issues**: None

## Quality Status
- **Build/test result**: Pass (all tests passing under `go test ./...` with firestore emulator)
- **Lint status**: Formatting checks pass via `go fmt`
- **Tests added/modified**: Added comprehensive tests in `internal/api/browse_test.go` checking all browse handler edge cases, authorization logic, and raw git data outputs.

## Loaded Skills
- None

## Key Decisions Made
- Reconstructed the full repository-relative path in `GetTree` by prepending the parent path to the relative path outputted by `git ls-tree -l --full-name <branch>:<path>`.
- Mapped client-facing auth errors in `authorizeGitRead` to return 401 Unauthorized for empty UID / lack of authentication and 403 Forbidden for incorrect username or lack of owner permissions.

## Artifact Index
- `/Users/treycaliva/projects/gitbucket/.agents/worker_browse/original_prompt.md` — The original task description and objectives.
