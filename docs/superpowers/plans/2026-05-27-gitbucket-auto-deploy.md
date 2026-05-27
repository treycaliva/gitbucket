# GitBucket Auto-Deploy via Cloud Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A push to `main` of the gitbucket repo (hosted in gitbucket itself) automatically builds and deploys the Go backend to Cloud Run and the React frontend to Firebase Hosting via gitbucket's existing Cloud Build engine; pushes to any other branch run compile/build CI only.

**Architecture:** Add a single branch-gated `cloudbuild.yaml` at the repo root (the engine at `internal/api/builds.go` already reads it on push, prepends an OIDC clone step, and submits to Cloud Build). Deploy steps no-op unless `${_BRANCH_NAME}` is `main`. A one-time idempotent `scripts/deploy-build-iam.sh` grants the build service account the roles it needs and creates an Artifact Registry repo. No changes to the Go build engine.

**Tech Stack:** Cloud Build YAML, Go (`yaml.v3` for a validation test only), bash + `gcloud`, Docker, `firebase-tools`, Artifact Registry, Cloud Run, Firebase Hosting.

**Reference spec:** `docs/superpowers/specs/2026-05-26-gitbucket-auto-deploy-design.md`

---

## File structure

- **Create `cloudbuild.yaml`** (repo root) — the build/deploy pipeline. Five steps expressed entirely under `steps:` (the only field the engine honors). Steps 1–2 always run (CI); steps 3–5 are guarded to run only on `main`.
- **Create `internal/api/cloudbuild_config_test.go`** — a Go test that strict-decodes `cloudbuild.yaml` using the same honored-field set as the engine, and asserts the expected step ids, branch guards, and image tag are present. This is the executable guard for the design's central constraint ("engine honors only `steps:`").
- **Create `scripts/deploy-build-iam.sh`** — one-time, idempotent IAM + Artifact Registry setup, styled after `scripts/deploy-infra.sh`.

### Engine facts the plan relies on (from `internal/api/builds.go`)

- `readCloudBuildYaml` reads `cloudbuild.yaml` at the pushed SHA.
- The engine parses only these per-step fields: `name`, `entrypoint`, `args`, `env`, `dir`, `id`, `waitFor`. It ignores top-level `images:`, `options:`, `substitutions:`, `timeout:`.
- The engine injects these substitutions: `_COMMIT_SHA`, `_BRANCH_NAME`, `_REPO_OWNER`, `_REPO_NAME`, `_GITBUCKET_URL`.
- The engine auto-prepends a shallow git clone of the branch into the Cloud Build `/workspace`, so all steps start with the repo checked out at the repo root.

---

## Task 1: Validation test for the CI steps (red)

**Files:**
- Test: `internal/api/cloudbuild_config_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/api/cloudbuild_config_test.go`:

```go
package api

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// cbStep mirrors the EXACT set of fields the engine honors in TriggerCloudBuild.
// Decoding with KnownFields(true) therefore fails if cloudbuild.yaml relies on
// any field the engine silently ignores (e.g. a top-level `images:` or `options:`).
type cbStep struct {
	Name       string   `yaml:"name"`
	Entrypoint string   `yaml:"entrypoint"`
	Args       []string `yaml:"args"`
	Env        []string `yaml:"env"`
	Dir        string   `yaml:"dir"`
	ID         string   `yaml:"id"`
	WaitFor    []string `yaml:"waitFor"`
}

type cbConfig struct {
	Steps []cbStep `yaml:"steps"`
}

// loadCloudBuildConfig reads the repo-root cloudbuild.yaml relative to this
// package directory (internal/api) and strict-decodes it.
func loadCloudBuildConfig(t *testing.T) cbConfig {
	t.Helper()
	data, err := os.ReadFile("../../cloudbuild.yaml")
	if err != nil {
		t.Fatalf("reading cloudbuild.yaml: %v", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg cbConfig
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("cloudbuild.yaml uses fields the engine does not honor (or is invalid): %v", err)
	}
	return cfg
}

func stepByID(cfg cbConfig, id string) (cbStep, bool) {
	for _, s := range cfg.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return cbStep{}, false
}

func TestCloudBuildYAMLOnlyUsesHonoredFieldsAndHasCISteps(t *testing.T) {
	cfg := loadCloudBuildConfig(t)

	for _, id := range []string{"backend-compile", "frontend-build"} {
		step, ok := stepByID(cfg, id)
		if !ok {
			t.Fatalf("expected a step with id %q", id)
		}
		if step.Name == "" {
			t.Errorf("step %q has no image name", id)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api -run TestCloudBuildYAMLOnlyUsesHonoredFieldsAndHasCISteps -v`
Expected: FAIL — `reading cloudbuild.yaml: open ../../cloudbuild.yaml: no such file or directory` (the file does not exist yet).

---

## Task 2: Create `cloudbuild.yaml` with the always-run CI steps (green)

**Files:**
- Create: `cloudbuild.yaml`

- [ ] **Step 1: Create `cloudbuild.yaml` with the two CI steps**

Create `cloudbuild.yaml` at the repo root:

```yaml
# Build/deploy pipeline for gitbucket, run by gitbucket's own Cloud Build engine
# (internal/api/builds.go). The engine prepends an OIDC clone of the pushed branch
# into /workspace, then runs the steps below in order.
#
# IMPORTANT: the engine honors ONLY the `steps:` array and, per step, only the
# fields name/entrypoint/args/env/dir/id/waitFor. Do NOT add top-level images:,
# options:, substitutions:, or timeout: — they are silently ignored.
#
# Substitutions injected by the engine: _COMMIT_SHA, _BRANCH_NAME, _REPO_OWNER,
# _REPO_NAME, _GITBUCKET_URL.
steps:
  # --- CI (runs on every branch) ---
  - id: backend-compile
    name: golang:1.25
    entrypoint: bash
    args:
      - -c
      - |
        set -e
        CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /dev/null main.go

  - id: frontend-build
    name: node:20
    entrypoint: bash
    dir: frontend
    args:
      - -c
      - |
        set -e
        npm ci
        npm run build
```

- [ ] **Step 2: Run the validation test to verify it passes**

Run: `go test ./internal/api -run TestCloudBuildYAMLOnlyUsesHonoredFieldsAndHasCISteps -v`
Expected: PASS.

- [ ] **Step 3: Sanity-check the YAML is well-formed**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('cloudbuild.yaml')); print('ok')"`
Expected: `ok`

- [ ] **Step 4: Commit**

```bash
git add cloudbuild.yaml internal/api/cloudbuild_config_test.go
git commit -m "feat(ci): add cloudbuild.yaml CI steps (backend compile, frontend build)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Validation test for the branch-gated deploy steps (red)

**Files:**
- Modify: `internal/api/cloudbuild_config_test.go`

- [ ] **Step 1: Add the failing deploy-guard test**

Append to `internal/api/cloudbuild_config_test.go`:

```go
func TestCloudBuildYAMLDeployStepsAreBranchGuarded(t *testing.T) {
	cfg := loadCloudBuildConfig(t)

	const imageRef = "us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:${_COMMIT_SHA}"
	const guard = `"${_BRANCH_NAME}" != "main"`

	deployIDs := []string{"image", "backend-deploy", "frontend-deploy"}
	for _, id := range deployIDs {
		step, ok := stepByID(cfg, id)
		if !ok {
			t.Fatalf("expected a deploy step with id %q", id)
		}
		script := strings.Join(step.Args, "\n")
		if !strings.Contains(script, guard) {
			t.Errorf("deploy step %q must be branch-guarded with %s so it no-ops off main", id, guard)
		}
	}

	// The image build, the backend deploy, and the pushed/deployed image must all
	// reference the same SHA-tagged Artifact Registry path.
	for _, id := range []string{"image", "backend-deploy"} {
		step, _ := stepByID(cfg, id)
		script := strings.Join(step.Args, "\n")
		if !strings.Contains(script, imageRef) {
			t.Errorf("step %q must reference the SHA-tagged image %q", id, imageRef)
		}
	}

	// CI steps must NOT carry the branch guard — they run on every branch.
	for _, id := range []string{"backend-compile", "frontend-build"} {
		step, _ := stepByID(cfg, id)
		script := strings.Join(step.Args, "\n")
		if strings.Contains(script, guard) {
			t.Errorf("CI step %q must not be branch-guarded", id)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api -run TestCloudBuildYAMLDeployStepsAreBranchGuarded -v`
Expected: FAIL — `expected a deploy step with id "image"` (deploy steps not added yet).

---

## Task 4: Add the branch-gated deploy steps to `cloudbuild.yaml` (green)

**Files:**
- Modify: `cloudbuild.yaml`

- [ ] **Step 1: Append the three deploy steps**

Append to `cloudbuild.yaml` (after the `frontend-build` step, inside the same `steps:` list):

```yaml
  # --- Deploy (main only; each step no-ops on other branches) ---
  - id: image
    name: gcr.io/cloud-builders/docker
    entrypoint: bash
    args:
      - -c
      - |
        if [ "${_BRANCH_NAME}" != "main" ]; then echo "skip: not main"; exit 0; fi
        set -e
        docker build -t "us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:${_COMMIT_SHA}" .
        docker push "us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:${_COMMIT_SHA}"

  - id: backend-deploy
    name: gcr.io/google.com/cloudsdktool/cloud-sdk
    entrypoint: bash
    args:
      - -c
      - |
        if [ "${_BRANCH_NAME}" != "main" ]; then echo "skip: not main"; exit 0; fi
        set -e
        gcloud run deploy gitbucket \
          --image "us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:${_COMMIT_SHA}" \
          --region us-central1 \
          --project git-bucket-79382 \
          --quiet

  - id: frontend-deploy
    name: node:20
    entrypoint: bash
    args:
      - -c
      - |
        if [ "${_BRANCH_NAME}" != "main" ]; then echo "skip: not main"; exit 0; fi
        set -e
        npx -y firebase-tools deploy --only hosting --project git-bucket-79382
```

Notes for the implementer:
- The guard comes *before* `set -e` so the `exit 0` skip path is clean on non-main branches.
- The deploy steps deliberately reference only engine-provided substitutions (`${_BRANCH_NAME}`, `${_COMMIT_SHA}`) and inline the full image path twice rather than introducing a new shell variable — this mirrors the proven clone step and avoids Cloud Build substitution ambiguity.
- `frontend-deploy` runs at the repo root (no `dir:`) so `firebase.json` (`public: frontend/dist`) and `.firebaserc` (default project `git-bucket-79382`) resolve. It reuses `frontend/dist` produced by `frontend-build` (the `/workspace` persists across steps).
- `frontend-deploy` authenticates `firebase-tools` via the build SA's Application Default Credentials from the metadata server — no token needed once `roles/firebasehosting.admin` is granted (Task 5).

- [ ] **Step 2: Run the deploy-guard test to verify it passes**

Run: `go test ./internal/api -run TestCloudBuildYAMLDeployStepsAreBranchGuarded -v`
Expected: PASS.

- [ ] **Step 3: Run both cloudbuild config tests together**

Run: `go test ./internal/api -run TestCloudBuildYAML -v`
Expected: PASS (both `...HonoredFieldsAndHasCISteps` and `...DeployStepsAreBranchGuarded`).

- [ ] **Step 4: Confirm the YAML still parses**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('cloudbuild.yaml')); print('ok')"`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add cloudbuild.yaml internal/api/cloudbuild_config_test.go
git commit -m "feat(cd): add branch-gated deploy steps to cloudbuild.yaml

Builds+pushes a SHA-tagged image, deploys Cloud Run, and deploys Firebase
Hosting only when _BRANCH_NAME is main.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: One-time IAM + Artifact Registry setup script

**Files:**
- Create: `scripts/deploy-build-iam.sh`

- [ ] **Step 1: Write the script**

Create `scripts/deploy-build-iam.sh`:

```bash
#!/usr/bin/env bash
# One-time, idempotent setup so the Cloud Build service account can deploy gitbucket.
# Grants the build SA the roles needed to push images, deploy Cloud Run, and deploy
# Firebase Hosting, and creates the Artifact Registry repo used by cloudbuild.yaml.
#
# Usage:
#   scripts/deploy-build-iam.sh
#   PROJECT_ID=git-bucket-79382 REGION=us-central1 SERVICE=gitbucket scripts/deploy-build-iam.sh
#
# If the gitbucket Cloud Run service sets CLOUD_BUILD_SERVICE_ACCOUNT, export the same
# value before running so the grants target the SA the builds actually run as.

set -euo pipefail

PROJECT_ID="${PROJECT_ID:-git-bucket-79382}"
REGION="${REGION:-us-central1}"
SERVICE="${SERVICE:-gitbucket}"
AR_REPO="${AR_REPO:-gitbucket}"

PROJECT_NUMBER="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')"

# The SA the builds run as: the engine uses CLOUD_BUILD_SERVICE_ACCOUNT if set,
# else the default Cloud Build SA.
BUILD_SA="${CLOUD_BUILD_SERVICE_ACCOUNT:-${PROJECT_NUMBER}@cloudbuild.gserviceaccount.com}"

# The Cloud Run runtime SA the new revision will run as; the build SA needs
# actAs on it to deploy. Fall back to the default compute SA.
RUNTIME_SA="$(gcloud run services describe "$SERVICE" \
  --region "$REGION" --project "$PROJECT_ID" \
  --format='value(spec.template.spec.serviceAccountName)' 2>/dev/null || true)"
if [[ -z "$RUNTIME_SA" ]]; then
  RUNTIME_SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
fi

echo "==> project=$PROJECT_ID build_sa=$BUILD_SA runtime_sa=$RUNTIME_SA"

# Project-level roles (add-iam-policy-binding is idempotent).
for ROLE in roles/run.admin roles/artifactregistry.writer roles/firebasehosting.admin; do
  echo "==> granting $ROLE to $BUILD_SA"
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:$BUILD_SA" \
    --role="$ROLE" \
    --condition=None \
    --quiet >/dev/null
done

# actAs on the runtime SA so 'gcloud run deploy' can set/keep it on the revision.
echo "==> granting roles/iam.serviceAccountUser on $RUNTIME_SA to $BUILD_SA"
gcloud iam service-accounts add-iam-policy-binding "$RUNTIME_SA" \
  --member="serviceAccount:$BUILD_SA" \
  --role="roles/iam.serviceAccountUser" \
  --project "$PROJECT_ID" \
  --quiet >/dev/null

# Artifact Registry repo for the image (idempotent: create only if missing).
if gcloud artifacts repositories describe "$AR_REPO" \
     --location="$REGION" --project="$PROJECT_ID" &>/dev/null; then
  echo "==> Artifact Registry repo '$AR_REPO' already exists"
else
  echo "==> creating Artifact Registry repo '$AR_REPO' in $REGION"
  gcloud artifacts repositories create "$AR_REPO" \
    --repository-format=docker \
    --location="$REGION" \
    --project="$PROJECT_ID" \
    --description="GitBucket container images"
fi

echo "==> done"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x scripts/deploy-build-iam.sh`
Expected: no output.

- [ ] **Step 3: Verify the script is syntactically valid**

Run: `bash -n scripts/deploy-build-iam.sh && echo "syntax ok"`
Expected: `syntax ok`

- [ ] **Step 4: Lint if shellcheck is available (optional but preferred)**

Run: `command -v shellcheck >/dev/null && shellcheck scripts/deploy-build-iam.sh && echo "shellcheck clean" || echo "shellcheck not installed — skipping"`
Expected: `shellcheck clean` or the skip message. Fix any warnings reported.

- [ ] **Step 5: Commit**

```bash
git add scripts/deploy-build-iam.sh
git commit -m "chore(infra): add one-time IAM + Artifact Registry setup for auto-deploy

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Live rollout & verification (run by the operator)

This task is executed against the real project, in order. It is the verification plan from the spec. Do not skip the feature-branch step — it proves the guard works before anything can deploy.

**Files:** none (operational).

- [ ] **Step 1: Confirm CI-only on a feature branch**

The current branch (`feat/auto-deploy-cloudbuild`, or wherever these commits land) is not `main`. Push it to the gitbucket origin:

Run: `git push origin HEAD`
Then open the commit in the gitbucket UI and watch the build (status comes from `commit_statuses` via `/api/webhooks/cloudbuild`; live logs stream over WebSocket).
Expected: `backend-compile` and `frontend-build` run and pass; `image`, `backend-deploy`, and `frontend-deploy` each print `skip: not main` and succeed. No new Cloud Run revision is created.

- [ ] **Step 2: Run the one-time IAM setup**

Run: `bash scripts/deploy-build-iam.sh`
Expected: ends with `==> done`. (If the gitbucket service sets `CLOUD_BUILD_SERVICE_ACCOUNT`, export the same value first so grants target the right SA.)

- [ ] **Step 3: Verify the grants landed**

Run:
```bash
gcloud projects get-iam-policy git-bucket-79382 \
  --flatten="bindings[].members" \
  --filter="bindings.role:(roles/run.admin OR roles/artifactregistry.writer OR roles/firebasehosting.admin)" \
  --format="table(bindings.role, bindings.members)"
```
Expected: the build SA appears for all three roles.

- [ ] **Step 4: Merge to main and deploy**

Merge the branch to `main` and push:
```bash
git checkout main
git merge --no-ff feat/auto-deploy-cloudbuild
git push origin main
```
Watch the build in the gitbucket UI.
Expected: all five steps run; `image` pushes `us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:<sha>`; `backend-deploy` creates a new Cloud Run revision; `frontend-deploy` succeeds.

**Primary risk to watch (from the spec):** the `frontend-deploy` step. If `firebase-tools` fails to authenticate via ADC, the fix is to add a CI token or service-account key in Secret Manager and pass it to the step (`FIREBASE_TOKEN` or `GOOGLE_APPLICATION_CREDENTIALS`). Capture the exact error before changing anything.

- [ ] **Step 5: Confirm the deploy is live**

Run:
```bash
gcloud run services describe gitbucket --region us-central1 --project git-bucket-79382 \
  --format="value(status.latestReadyRevisionName, status.url)"
```
Then load `https://gitbucket.dev` (and/or `https://git-bucket-79382.web.app`) and confirm the new frontend serves and the backend `/api` responds from the new revision.
Expected: a fresh `latestReadyRevisionName` and a working site.

- [ ] **Step 6: Decommission the manual path (doc update)**

Update `scripts/deploy.sh`'s header comment (and the deploy-workflow note) to record that pushing to `main` now auto-deploys, and that `deploy.sh` is the manual fallback / break-glass path. Commit:

```bash
git add scripts/deploy.sh
git commit -m "docs: note that push-to-main now auto-deploys; deploy.sh is the fallback

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Notes on conventions & gotchas

- **Do not** add top-level `images:`, `options:`, `substitutions:`, or `timeout:` to `cloudbuild.yaml` — the engine ignores them, so they create false confidence. The image must be pushed by the explicit `docker push` in the `image` step (covered), and the 60-minute Cloud Build default timeout applies (sufficient).
- The build runs after the engine prepends a shallow clone, so all steps begin at the repo root with the source present; `frontend-build` writes `frontend/dist`, which `frontend-deploy` reuses from the persistent `/workspace`.
- Cloud Run image-only deploys preserve existing env vars, IAM, and scaling — the deploy does not need to re-specify them.
- Self-deploy is safe: Cloud Build runs in Google infra (not on the Cloud Run instance) and Cloud Run revisions are atomic, so deploying the new revision never disrupts the in-flight build or its status callback.
```
