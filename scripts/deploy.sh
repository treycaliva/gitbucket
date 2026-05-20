#!/usr/bin/env bash
# Build and deploy GitBucket to Cloud Run via Cloud Build (source deploy).
# Preserves existing env vars / IAM / scaling on the service.
#
# Usage:
#   scripts/deploy.sh                    # deploy to default service
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

echo "==> deploying $SERVICE to $REGION (project=$PROJECT_ID, sha=$GIT_SHA)"

gcloud run deploy "$SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --source=. \
  --quiet

echo "==> done"
gcloud run services describe "$SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --format="value(status.url)"
