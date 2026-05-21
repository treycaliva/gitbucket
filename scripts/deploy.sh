#!/usr/bin/env bash
# Build and deploy GitBucket: Go backend to Cloud Run, React frontend to Firebase Hosting.
# Preserves existing env vars / IAM / scaling on the service.
#
# Usage:
#   scripts/deploy.sh                    # deploy to default service and hosting
#   SERVICE=gitbucket-staging scripts/deploy.sh
#   REGION=us-east1 scripts/deploy.sh

set -euo pipefail

PROJECT_ID="${PROJECT_ID:-git-bucket-79382}"
REGION="${REGION:-us-central1}"
SERVICE="${SERVICE:-gitbucket}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
  echo "warning: uncommitted changes in working tree — deploying them anyway" >&2
fi

GIT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

# Pre-flight backend check: compile the Go binary locally so we catch syntax errors early
echo "==> pre-flight: go build check"
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /dev/null main.go

# Pre-flight frontend check: build the static React assets locally
echo "==> building frontend assets"
(cd frontend && npm install && npm run build)

echo "==> deploying Go backend $SERVICE to Cloud Run in $REGION (project=$PROJECT_ID, sha=$GIT_SHA)"
gcloud run deploy "$SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --source=. \
  --quiet

echo "==> deploying React frontend to Firebase Hosting"
npx -y firebase-tools deploy --only hosting --project "$PROJECT_ID"

echo "==> done"
gcloud run services describe "$SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --format="value(status.url)"
