# GitBucket — Private Beta Readiness Assessment

*Assessed 2026-07-11 against `main` (b12088e). Verdict: **close, but not ready yet** — the product surface is beta-quality, but there are 3 blockers (one security, two operational) and a handful of should-fixes that are cheap relative to the risk they remove.*

## Overall verdict

The core product — Git over Smart HTTP, pull requests with reviews, branch protection with CODEOWNERS, LFS, CI statuses, collaborators, 2FA — is genuinely feature-complete and beyond typical MVP scope. Auth fundamentals are sound: PATs are stored as SHA-256 hashes of 160-bit random tokens, Firebase verification is correct, and every core web/git/LFS write path enforces ownership. `go build ./...` and `go test ./internal/...` pass cleanly (62 test files, ~0.69 test:source ratio).

What blocks the beta is one authorization gap in the newest surface (the GitHub-App v3 API), and two Cloud Run operational risks that will bite under real multi-user load.

---

## Blockers

### 1. Cross-tenant IDOR in the v3 GitHub-App API (security)

Every handler under `/api/v3/*` checks only the installation token's *permission scope* (`RequirePerm`) and never validates that the `{owner}/{repo}` in the URL belongs to the installation's account or its `RepositoryIDs`. The `InstallationContext` carries `Account` and `RepositoryIDs` (`internal/api/v3/middleware.go:14`, `types.go:92`), but no handler consults them — including the write path `CreatePull` (`internal/api/v3/pulls.go:63`) and the read paths registered in `routes.go:32-39`. Any valid installation token can read or write **any** repository, including other users' private repos, by editing the URL.

**Fix:** add a middleware that resolves `{owner}/{repo}` and rejects requests outside the installation's account/repo selection — or gate `/api/v3/*` off entirely for the beta.

### 2. Unbounded `/tmp/repos` growth on Cloud Run (stability)

`LOCAL_REPOS_ROOT` defaults to `/tmp/repos` (`internal/config/config.go:48`), and materialized repos are never evicted during an instance's lifetime (cleanup only happens on explicit repo delete). On Cloud Run, `/tmp` is an in-memory tmpfs that counts against the memory limit — a busy instance touching many or large repos will OOM.

**Fix:** LRU eviction or post-request cleanup of the scratch dir; at minimum, size-cap the cache and delete least-recently-synced repos.

### 3. No CI at all

There is no `.github/` directory — nothing runs `go build`, `go test`, lint, or the E2E suite on push or PR. (`sync-workflow.yaml` is a GCP Workflows runtime definition, not CI.) Shipping a beta and taking fixes quickly without an automated test gate is how regressions reach users.

**Fix:** a minimal GitHub Actions workflow running `go build ./...` + `go test ./internal/...` + `npm run build` on every PR; ideally also the 13-scenario E2E harness (`tests/test_e2e.go`) against the Firestore emulator.

---

## Should fix before invites go out

1. **Stale-ref resurrection across instances.** `gcs.DownloadRepo` is a merge, not a mirror — it never deletes local files absent from GCS (`internal/gcs/gcs.go:70-119`), while `UploadRepo` *does* delete GCS files missing locally. A stale instance can retain a deleted/force-rewound ref and resurrect it on its next push. Until `DownloadRepo` prunes, run the beta with `max-instances=1`.
2. **Two competing lock implementations share one Firestore doc.** The push path uses `db.AcquireLock` with local wall-clock expiry (`internal/db/db.go:410`); the GitHub-sync path uses `AcquireLeasedLock` with Firestore server time (`internal/sync/locks.go:37`). Both write `locks/{owner}_{repo}` with different field schemas. Consolidate on the server-time variant.
3. **No rate limiting anywhere** (`main.go:250-254`). PAT Basic-Auth is brute-forceable in principle (tokens are 160-bit so the real risk is abuse/cost, not compromise). Add a basic limiter before opening signups.
4. **Unguarded detached goroutines + Cloud Run CPU throttling.** `go h.TriggerCloudBuild(...)` (`internal/api/git.go:421`) and the sync goroutine (`internal/api/sync.go:366`) have no `recover()` — a panic kills the instance — and post-response work gets CPU-throttled unless the service runs CPU-always-allocated. Add `recover()` and set `--no-cpu-throttling` (or move the work to Cloud Tasks).
5. **Silent partial failures after push.** GCS upload or the Firestore `updatedAt` bump failing after a push is only logged (`internal/api/git.go:379-390`); a missed `updatedAt` bump means other instances never re-sync. Surface these as errors and retry.
6. **DEV_MODE is a full auth bypass, read ad-hoc in multiple places.** It defaults off and isn't set in the Dockerfile or deploy script (good), but `git.go:161`, `builds.go:665`, `sync.go:213`, and `api.go:1198` each re-read it from the env. One accidental `DEV_MODE=true` on Cloud Run disables all auth. Add a startup refusal to boot with DEV_MODE outside emulator contexts.
7. **Webhook auth is optional when unconfigured.** `HandleGitHubWebhook` processes payloads without HMAC verification if `GITHUB_WEBHOOK_SECRET` is unset (`internal/api/webhooks.go:96-105`). Make the secret mandatory in prod.
8. **Health check is liveness-only.** `/api/health` returns static JSON (`internal/api/api.go:72`); it can't tell Cloud Run the instance lost Firestore/GCS. Add a readiness probe.

## Product gaps to set expectations on (not blockers)

- **No issue tracker** — no backend or UI at all.
- **No search** beyond client-side repo-name filtering on the dashboard.
- **Activity tab is deliberately inert** (`frontend/src/App.jsx:367`) — hide it for the beta.
- **README rendering is a regex-based converter** (`Repository.jsx:107`), not a real markdown renderer — complex READMEs will render imperfectly. Consider `marked`/`micromark` + sanitizer.
- **Rejected pushes return HTTP 200.** Branch-protection enforcement runs after the CGI streams its response (documented in `PROJECT.md`), so a blocked push *looks* successful at the CLI and silently never lands. Correct by design, but users will file this as a bug — at minimum document it prominently.
- **No in-app help for git-over-HTTPS auth.** The "your git password is a PAT" fact lives only in the repo README. Add a short help blurb near the clone URL / tokens page.

## Cleanup

- Delete the legacy Node implementation: `server/` (including a checked-in ~50 MB `main` binary) and the root `package.json`. It's excluded from deploy but bloats the repo and duplicates auth logic that will confuse audits.
- Remove `frontend/lint_output.txt` and do a lint pass.
- Wildcard CORS (`main.go:125`) — not a credential-theft vector with bearer/PAT auth, but pin it to the hosting origin anyway.
- `RESTRICTED_IP` gating trusts a spoofable `X-Forwarded-For` and is default-open (`main.go:45-85`) — treat it as defense-in-depth only, never as an auth control.

## What's already in good shape

- **Auth core:** correct Firebase ID-token verification; PATs generated from `crypto/rand`, stored hashed, owner-scoped revocation; PAT bound to the Basic-Auth username on every git/LFS request.
- **Authorization on the core surface:** ownership/collaborator checks verified on every repo, branch, collaborator, branch-protection, sync, PR-merge, git-push, and LFS path.
- **No secrets in the repo:** no tracked `.env`, keys, or service-account files; the frontend fetches Firebase config from `/api/config` at runtime.
- **Tests:** all unit tests pass; heavy coverage on the apps/db/v3 packages; a real 13-scenario E2E harness exists (manual for now).
- **Design decisions documented:** GCS versioning-off rationale, soft-delete recovery, push-time protection enforcement — all written down in `PROJECT.md`.

## Suggested launch checklist

- [ ] Scope-check `/api/v3/*` against the installation account (or gate the surface off)
- [ ] Bound `/tmp/repos` (eviction) — or accept `max-instances=1` for the beta
- [ ] Add GitHub Actions: build + unit tests on PR (E2E as a follow-up)
- [ ] Prune stale local refs in `DownloadRepo` (or pin `max-instances=1`)
- [ ] Rate limiter on auth-bearing endpoints
- [ ] `recover()` in detached goroutines; deploy with CPU always allocated
- [ ] Make `GITHUB_WEBHOOK_SECRET` mandatory; startup refusal on stray `DEV_MODE=true`
- [ ] Hide the Activity tab; add PAT/clone help blurb
- [ ] Delete `server/`, root `package.json`, `frontend/lint_output.txt`
- [ ] Confirm Cloud Run `--timeout` is high enough for large pushes/clones and how `frontend/dist` ships (the Dockerfile doesn't copy it)
