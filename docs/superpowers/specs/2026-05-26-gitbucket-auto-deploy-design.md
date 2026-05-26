# GitBucket Auto-Deploy via Cloud Build (Dogfood)

**Date:** 2026-05-26
**Status:** Approved design, ready for implementation plan

## Goal

Replace the manual `scripts/deploy.sh` workflow with an automated deploy that runs
through gitbucket's own already-shipped Cloud Build engine. A push to `main` of the
gitbucket repo (which is hosted inside gitbucket itself, `treycaliva/gitbucket` on the
Cloud Run `origin` remote) automatically builds and deploys both halves of the app:
the Go backend to Cloud Run and the React frontend to Firebase Hosting. Pushes to any
other branch run compile/build checks only (CI), no deploy.

This is the "dogfood deploy" scope: gitbucket deploys gitbucket. It is **not** a
generic per-repo deployment feature — that may come later but is explicitly out of
scope here.

## Background: what already exists

The CI/build engine is already implemented and deployed:

- `internal/api/git.go:421` — on every push, `go h.TriggerCloudBuild(owner, repo, branch, sha)` fires.
- `internal/api/builds.go:262` `TriggerCloudBuild` — reads `cloudbuild.yaml` from the
  pushed commit (`readCloudBuildYaml`, hardcoded to that path), prepends an OIDC-authenticated
  shallow-clone step that clones the branch back out of gitbucket into `/workspace`, then
  submits a Cloud Build.
- `internal/api/builds.go:663` `HandleCloudBuildWebhook` (route `/api/webhooks/cloudbuild`
  in `api.go:77`) — receives Pub/Sub status pushes and updates the `commit_statuses`
  Firestore collection; live logs stream over WebSocket.

Today this engine no-ops for gitbucket because there is no `cloudbuild.yaml` in the repo.

## Key engine constraint (drives the whole design)

`TriggerCloudBuild` parses **only the `steps:` array** from `cloudbuild.yaml`, and per
step only the fields `Name`, `Entrypoint`, `Args`, `Env`, `Dir`, `ID` (`id`), and
`WaitFor` (`waitFor`). It **ignores** top-level `images:`, `options:`, `substitutions:`,
and `timeout:`.

Consequences, all of which this design respects:

1. The image must be pushed by an explicit `docker push` step — `images:` is not honored.
2. Branch gating and test→deploy ordering must live inside the steps (bash guards +
   sequential ordering), not in `options` or trigger config.
3. The Cloud Build API default `timeout` (60 minutes) applies. The full pipeline fits
   well within this; no engine change needed.

Substitutions the engine injects and that steps may rely on: `_COMMIT_SHA`,
`_BRANCH_NAME`, `_REPO_OWNER`, `_REPO_NAME`, `_GITBUCKET_URL`.

## Architecture & flow

```
push to gitbucket origin (any branch)
        │
        ▼
git.go:421  go h.TriggerCloudBuild(owner, repo, branch, sha)
        │   reads cloudbuild.yaml @ sha, prepends OIDC clone step, submits to Cloud Build
        ▼
Cloud Build runs steps (as the build service account):
   [auto-prepended]  clone branch @ sha into /workspace
   step 1  backend compile check         (always)
   step 2  frontend build                (always — produces frontend/dist)
   step 3  docker build + push image      (main only)
   step 4  gcloud run deploy --image      (main only)
   step 5  firebase deploy --only hosting (main only)
        │
        ▼
Pub/Sub → /api/webhooks/cloudbuild → commit_statuses (status in UI, live logs over WS)
```

Branch gating: each main-only step begins with
`[ "$_BRANCH_NAME" = "main" ] || { echo "skip: not main"; exit 0; }`. Feature branches
therefore get compile + frontend-build CI for free; only `main` proceeds through deploy.
Steps run sequentially and any failure aborts the build, so a broken compile or frontend
build blocks the deploy.

Self-deploy safety: Cloud Run revisions are atomic (traffic shifts to the new healthy
revision), and the build executes in Google's Cloud Build infra — not on the Cloud Run
instance — so deploying the new revision does not disrupt the in-flight build or its
status callback. The new revision still serves `/api/webhooks/cloudbuild`, so status
updates continue to land.

## Component 1: `cloudbuild.yaml` (new, repo root)

Expressed entirely as `steps:` (the only honored field). Each step's purpose, image, and
command:

- **step 1 — backend compile (always).** Image `golang:1.25`. Run
  `CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /dev/null main.go`. Mirrors the
  `deploy.sh` preflight. Gates on the backend compiling.
- **step 2 — frontend build (always).** Image `node:20`. `dir: frontend`, run
  `npm ci && npm run build`. Validates the SPA compiles and produces `frontend/dist` for
  step 5.
- **step 3 — image build + push (main only).** Image `gcr.io/cloud-builders/docker`.
  Guard, then `docker build` the existing repo `Dockerfile` tagged
  `us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:$_COMMIT_SHA`, then
  `docker push` the same tag.
- **step 4 — backend deploy (main only).** Image
  `gcr.io/google.com/cloudsdktool/cloud-sdk`. Guard, then
  `gcloud run deploy gitbucket --image us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:$_COMMIT_SHA --region us-central1 --project git-bucket-79382 --quiet`.
  Image-only deploy preserves existing env vars, IAM, and scaling on the service.
- **step 5 — frontend deploy (main only).** Image `gcr.io/google.com/cloudsdktool/cloud-sdk`
  (has `npx`/node) or `node:20`. Guard, then
  `npx -y firebase-tools deploy --only hosting --project git-bucket-79382`, authenticating
  via the build SA's Application Default Credentials (metadata server). Uses the committed
  `firebase.json` (`public: frontend/dist`) and `.firebaserc` (default project
  `git-bucket-79382`).

Notes:
- Tag images by `$_COMMIT_SHA` for traceability and rollback (`gcloud run deploy` to a
  prior tag).
- No reliance on `images:`, `options:`, `substitutions:`, or `timeout:` (engine ignores them).

## Component 2: `scripts/deploy-build-iam.sh` (new, one-time setup)

Idempotent, styled after `scripts/deploy-infra.sh`. Resolves the build service account
(default `<PROJECT_NUMBER>@cloudbuild.gserviceaccount.com`, or the value of
`CLOUD_BUILD_SERVICE_ACCOUNT` if the gitbucket service sets it) and grants:

- `roles/artifactregistry.writer` — push the image.
- `roles/run.admin` — deploy the Cloud Run revision.
- `roles/iam.serviceAccountUser` on the Cloud Run runtime service account — deploy "as" it.
- `roles/firebasehosting.admin` — deploy Hosting.

Plus a one-time Artifact Registry repo creation (idempotent):
`gcloud artifacts repositories create gitbucket --repository-format=docker --location=us-central1`.

Variables overridable by env (`PROJECT_ID`, `REGION`, `SERVICE`) matching existing scripts.

## Error handling

- Compile failure (step 1) or frontend build failure (step 2) aborts the build before any
  deploy; status reported as failed via the existing webhook → `commit_statuses` path.
- Image push failure (step 3) aborts before deploy.
- A failed `gcloud run deploy` leaves the previous revision serving traffic (atomic).
- A failed Hosting deploy after a successful backend deploy leaves backend updated and
  frontend stale — surfaced as a failed build; remediation is re-running/fixing. (Acceptable
  for dogfood; ordering backend-before-frontend means the API contract is forward-compatible
  before new assets land.)

## Risks

1. **Firebase auth via ADC.** Modern `firebase-tools` authenticates from the metadata
   server's ADC, so `roles/firebasehosting.admin` on the build SA should suffice with no
   token. This is the one integration point not verifiable by reading code and is the
   **primary validation target** on first real run. Fallback: store a CI token or service
   account key in Secret Manager and pass via `FIREBASE_TOKEN` / `GOOGLE_APPLICATION_CREDENTIALS`.
2. **Build timeout.** Engine ignores `timeout:`; the 60-minute API default applies and the
   pipeline fits well within it. If a tighter/looser bound is ever needed, that requires a
   one-line `build.Timeout` addition in `builds.go` — out of scope now.

## Out of scope (YAGNI)

- Generic per-repo deployment semantics / environments for non-gitbucket repos.
- Staging environment and manual prod promotion (chosen model is auto-on-main).
- Full Firestore-emulator-backed test suite as a deploy gate (compile-only for now;
  emulator test step is a clean future follow-up).
- Any change to the Go build engine (`builds.go`, `git.go`).

## Verification plan

Because this is the deploy path itself, validate incrementally:

1. Push a trivial commit to a **feature branch**; confirm in the gitbucket UI / Cloud Build
   that only steps 1–2 run and pass, and steps 3–5 print "skip: not main".
2. Run `scripts/deploy-build-iam.sh` once to grant roles and create the AR repo.
3. Push to **main**; confirm the full chain runs: image pushed to Artifact Registry, a new
   Cloud Run `gitbucket` revision deployed, Firebase Hosting updated, and the build status
   surfaced on the commit in the gitbucket UI.
4. Confirm the deployed site (`gitbucket.dev` / `git-bucket-79382.web.app`) serves the new
   frontend and the backend `/api` responds from the new revision.
