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
