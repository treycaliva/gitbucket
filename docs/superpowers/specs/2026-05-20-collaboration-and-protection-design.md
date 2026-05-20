# Collaboration & Branch Protection — Design

**Date:** 2026-05-20
**Status:** Approved for planning
**Scope:** Five independent-but-related workstreams: Quickstart relocation, lightweight collaborators, GitLab-style branch protection, CODEOWNERS support, GCS hardening.

## Background

GitBucket today only models a single `owner` per repository. Push authorization is owner-only (`internal/api/git.go:160`), and there is no concept of reviewers, approvals, branch rules, or per-path ownership. To move toward a usable mini-GitHub/GitLab, we need:

1. A real notion of additional users with repo access (collaborators).
2. Branch-level safety rules that gate pushes and merges.
3. Per-path code ownership for auto-routing reviewers and gating PR merges.

Plus two unrelated UX/infra cleanups: the empty-state "Repository Command Quickstart" block lives on the Settings tab where it does not belong, and we want to make an explicit decision about GCS object versioning.

## Goals

- Owner can add/remove collaborators by username from Settings.
- Owner can configure per-branch-pattern protection rules (push/merge allowlists, required approvals, codeowner gate, force-push/delete blocks).
- PRs auto-request reviewers based on a CODEOWNERS file, and merge can be gated on codeowner approval.
- Quickstart hint surfaces only when the repo is empty, on the Code tab.
- A documented decision on GCS versioning and a script to enable bucket soft-delete.

## Non-goals

- Role hierarchy (Maintainer / Developer / Reporter). Collaborators are a flat "has access" list in v1.
- Teams or org-level permissions.
- Required status checks (CI integration).
- Linked-issue requirements.
- Web-based review UI beyond a single Approve / Request-changes button.

---

## A. Quickstart relocation

**Current:** `frontend/src/pages/Repository.jsx:625` renders a "Repository Command Quickstart" card on the Settings tab.

**Change:**
- Remove the block from the Settings tab.
- Render the same block on the Code tab when the repo has zero commits (`commits.length === 0` after data load).
- Use the existing empty-state region.

**No backend changes.** Owner-only visibility is preserved by gating on `isOwner` as today.

---

## B. Lightweight collaborators

### Firestore schema

Repository document gains:

```
collaborators: [
  { uid: string, username: string, addedAt: timestamp, addedBy: string }
]
```

Stored as an array on the existing `repositories/{repoId}` document — small (typical <50 entries), atomic updates, no sub-collection needed.

### REST API

| Method | Path | Auth | Behavior |
| ------ | ---- | ---- | -------- |
| `GET` | `/api/repos/{owner}/{repo}/collaborators` | OptionalWebAuth (private repos require access) | List collaborators. |
| `POST` | `/api/repos/{owner}/{repo}/collaborators` | RequireWebAuth + owner | Body: `{ username }`. Looks up the user in Firestore by username, rejects if not found, appends to array if not present. |
| `DELETE` | `/api/repos/{owner}/{repo}/collaborators/{username}` | RequireWebAuth + owner | Remove by username. |

### Authorization helper

New `internal/auth/repo_access.go`:

```go
type RepoAccess struct { IsOwner, IsCollaborator bool }
func HasRepoAccess(meta *db.RepositoryMetadata, uid string) RepoAccess
func CanPush(meta *db.RepositoryMetadata, uid string) bool       // owner || collaborator
func CanRead(meta *db.RepositoryMetadata, uid string) bool       // public || owner || collaborator
```

Call sites to update:
- `internal/api/git.go:160` (push auth)
- `internal/api/git.go:168` (private read)
- `internal/api/git.go:411` (LFS push)
- `internal/api/git.go:421` (LFS private read)
- `internal/api/git.go:527` (LFS upload)
- `internal/api/git.go:599` (private repo read)

Branch-protection rules (Workstream C) layer on top of these — `CanPush` is the floor, BP narrows further by branch.

### UI

Settings tab gains a new "Collaborators" card above "Repository Settings":
- Text input + Add button (username).
- List of current collaborators with Remove button per row.
- Optimistic update, surface errors inline.

---

## C. Branch protection (GitLab-style)

### Firestore schema

Sub-collection `repositories/{repoId}/branchProtection/{ruleId}`:

```
{
  pattern: string,              // glob, e.g. "main", "release/*", "*"
  pushAllowlist: [uid],         // empty = no direct push allowed
  mergeAllowlist: [uid],        // empty = no merges allowed; UI default is "all collaborators + owner"
  requirePullRequest: bool,
  requireApprovals: int,        // 0 = no minimum
  requireCodeownerApproval: bool,
  blockForcePush: bool,
  blockDeletion: bool,
  createdAt, createdBy,
  updatedAt, updatedBy
}
```

`ruleId` is a deterministic hash of `pattern` so duplicate patterns are impossible.

### Pattern matching

`filepath.Match` semantics — supports `*`, `?`, `[…]`. Multiple rules can match the same ref; resolution is deterministic via this ordering on matched rules:

1. Fewer wildcard characters (`*`, `?`, `[`) wins.
2. Tie → longer pattern wins.
3. Tie → lexicographically first pattern wins.

Owner has no implicit bypass — must appear in `pushAllowlist` / `mergeAllowlist` to perform the action. The UI defaults the owner into both lists when creating a new rule.

### REST API

| Method | Path | Auth | Behavior |
| ------ | ---- | ---- | -------- |
| `GET` | `/api/repos/{owner}/{repo}/branch-protection` | OptionalWebAuth (read access) | List rules. |
| `POST` | `/api/repos/{owner}/{repo}/branch-protection` | Owner | Create rule. |
| `PUT` | `/api/repos/{owner}/{repo}/branch-protection/{ruleId}` | Owner | Replace rule. |
| `DELETE` | `/api/repos/{owner}/{repo}/branch-protection/{ruleId}` | Owner | Delete rule. |

### Push-path enforcement

New `internal/git/protection.go`:

```go
type RefUpdate struct { RefName, OldSha, NewSha string; IsForce, IsDelete bool }
type EnforceResult struct { Rejected []RefUpdate; Reasons map[string]string }
func EnforcePush(ctx, rules []Rule, updates []RefUpdate, pusherUid string) EnforceResult
```

Integrated in `internal/api/git.go` receive-pack handler. The current sync flow (parse pack → materialize → run git-http-backend → sync back) gains a **pre-sync-back** validation step:

1. After `git-http-backend` writes the new objects to the materialized repo, but **before** ref updates are written back to Firestore and objects synced to GCS, parse the proposed ref updates from the CGI output / repo state diff.
2. For each ref update, compute `IsForce` (new SHA is not a descendant of old SHA) and `IsDelete` (new SHA = zero).
3. Apply `EnforcePush`. If any update is rejected, **abort the whole push** (reset local repo to pre-push state, return an HTTP error in the receive-pack response stream).

This means a single rejected ref aborts the entire push — matches Git's all-or-nothing semantics.

### Merge-path enforcement

The PR merge handler creates ref updates directly (it does not go through receive-pack), so its checks are the authority for merge-induced ref changes. In `internal/api/pull_requests.go` merge handler, before performing the merge:
1. Load matching rule for `targetBranch` (or no rule → skip).
2. Check user uid is in `mergeAllowlist`.
3. Count approvals → must meet `requireApprovals`.
4. If `requireCodeownerApproval`, verify at least one approval comes from a CODEOWNERS-matched user for the PR's changed files (see Workstream D).
5. If any check fails, return 403 with the specific reason.

### UI

Settings tab gains a "Branch protection" card under Collaborators:
- Table of current rules: pattern, push allowlist count, merge allowlist count, badges for each toggle.
- "Add rule" button opens a modal with full form.
- Edit/Delete actions per row.

---

## D. CODEOWNERS support

### Parser

New `internal/git/codeowners.go`:

```go
type Rule struct { Pattern string; Owners []string; LineNo int }
type CodeOwners struct { Rules []Rule }
func Parse(r io.Reader) (*CodeOwners, error)
func (c *CodeOwners) Match(path string) []string  // last-matching-rule wins
```

Syntax (GitHub-compatible subset):
- Comments: `#` to end of line.
- Each rule: `<pattern> @user1 @user2 …` (whitespace-separated).
- Pattern syntax: glob with `*`, `**`, `?`. Leading `/` anchors to repo root. Trailing `/` matches directory.
- Last matching rule wins (overrides earlier rules).
- Owners with `@` prefix only — no team or email syntax in v1.

### File resolution

Lookup order in the materialized working tree:
1. `CODEOWNERS`
2. `.gitbucket/CODEOWNERS`
3. `docs/CODEOWNERS`

First found wins. If none exist, treated as empty (no owners).

### PR data model

`PullRequest` gains:
```
requestedReviewers: [username]   // computed at PR open, immutable
```

New subcollection `repositories/{repoId}/pulls/{n}/reviews/{uid}`:
```
{ uid, username, state: "approved" | "changes_requested" | "commented", submittedAt, body }
```

### Integration points

**PR open** (`internal/api/pull_requests.go` `CreatePullRequest`):
1. After PR doc is created, compute changed files via `git diff --name-only sourceBranch...targetBranch` in the materialized repo.
2. Load CODEOWNERS, resolve owners for each changed file, union to a unique set.
3. Remove the PR author from the set.
4. Update PR doc with `requestedReviewers`.

**PR merge** (existing merge handler):
- When target branch's protection has `requireCodeownerApproval = true`, query reviews subcollection and verify ≥1 approval from a user whose username appears in `requestedReviewers` (or in CODEOWNERS for any changed file — recompute to handle post-open changes? **v1: trust `requestedReviewers` snapshot** for simplicity).

**File browser display** (Code tab):
- New endpoint `GET /api/repos/{owner}/{repo}/codeowners?path={dir}&ref={sha}` returning `{ entries: { "filename": ["@alice", "@bob"], … } }`.
- UI renders owner handles next to file/dir rows. Cached per-listing render.

### Review API

| Method | Path | Auth | Behavior |
| ------ | ---- | ---- | -------- |
| `POST` | `/api/repos/{owner}/{repo}/pulls/{n}/reviews` | RequireWebAuth + read access | Body: `{ state, body }`. Rejects if user is the PR author. Upserts the user's review (one per user). |
| `GET` | `/api/repos/{owner}/{repo}/pulls/{n}/reviews` | OptionalWebAuth | List reviews. |

### UI

PR detail page gains:
- "Reviewers" section listing `requestedReviewers` with their current review state.
- "Approve" / "Request changes" / "Comment" buttons for the signed-in user (hidden for the author).
- Merge button shows blocking reason when protection gates merge.

---

## E. GCS hardening

### Decision: no object versioning

Rationale (to be documented in `PROJECT.md` and referenced from `CLAUDE.md`):

> Git objects (loose, packfiles) and LFS blobs are content-addressed by SHA — they are never overwritten, only added. GCS object versioning protects against overwrite and accidental delete of mutable objects; with immutable content-addressed storage, versioning provides no recovery benefit. The only mutable state worth recovering — refs and repo metadata — lives in Firestore, not GCS. Enabling versioning would multiply storage cost (every GC/repack cycle leaves dead pack versions) without any practical recovery upside.

### Soft-delete instead

Enable GCS bucket soft-delete with 7-day retention. This guards against accidental bulk-delete or a bug in GC/repack code without per-object versioning overhead.

New script `scripts/enable-soft-delete.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
: "${GCS_BUCKET:?must be set}"
gcloud storage buckets update "gs://${GCS_BUCKET}" --soft-delete-duration=7d
```

Idempotent — safe to re-run. Documented in `PROJECT.md`.

---

## Cross-cutting concerns

### Dependency order

```
A (Quickstart)            ── independent
E (GCS hardening)         ── independent
B (Collaborators)         ── blocks C, D
C (Branch protection)     ── blocks D's merge enforcement
D (CODEOWNERS)            ── parser/PR-open independent; merge-enforcement after C
```

### Testing

- **Go unit tests**: each new package (`internal/auth/repo_access`, `internal/git/protection`, `internal/git/codeowners`) gets table-driven tests.
- **API integration tests**: extend `internal/api/*_test.go` with collaborator CRUD, BP CRUD, push rejection, PR merge gating, codeowner auto-request.
- **E2E**: `tests/test_e2e.go` gains scenarios:
  - Force-push to protected branch is rejected.
  - Direct push by allowlisted collaborator succeeds.
  - PR merge blocked by missing codeowner approval.
  - PR merge succeeds after codeowner approves.

### REST contract

Every new endpoint and PR response shape change gets logged in `PROJECT.md` §Interface Contracts per the project convention.

### Username/pattern validation

- Collaborator usernames: same regex as existing `^[a-zA-Z0-9-]{3,20}$`.
- Branch-protection patterns: validated to be non-empty, ≤200 chars, `filepath.Match` syntax-checked at write time.

### Backward compatibility

- Repos without `collaborators` field: treated as empty list.
- Repos without `branchProtection` subcollection: no rules enforced (current behavior preserved).
- PRs without `requestedReviewers`: rendered as empty list; merge gates that require codeowner approval will block until at least one applicable approval exists.

---

## Out of scope (deferred)

- Required CI status checks.
- Linked-issue requirements before merge.
- Review threads / inline comments.
- Team-based owners in CODEOWNERS (`@org/team`).
- Push rules beyond branch protection (commit message regex, signed commits, file-size limits).
- Granting collaborators write access to branch-protection settings (owner-only in v1).
