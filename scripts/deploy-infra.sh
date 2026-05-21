#!/usr/bin/env bash
# Deploy Google Cloud KMS, Workflows, and configure IAM permissions for GitBucket.
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-git-bucket-79382}"
REGION="${REGION:-us-central1}"
KEYRING="${KEYRING:-gitbucket-keyring}"
KEY="${KEY:-gitbucket-key}"
WORKFLOW_NAME="${WORKFLOW_NAME:-gitbucket-sync-workflow}"
RUN_SERVICE="${RUN_SERVICE:-gitbucket}"

echo "==> Configuring infrastructure for GitBucket in project=$PROJECT_ID, region=$REGION"

# 1. Enable Google Cloud APIs (Skipped: APIs are already enabled on project)
# echo "==> Enabling APIs..."
# gcloud services enable \
#   kms.googleapis.com \
#   workflows.googleapis.com \
#   workflowexecutions.googleapis.com \
#   cloudbuild.googleapis.com \
#   --project="$PROJECT_ID"

# 2. Setup Cloud KMS for credential encryption
echo "==> Setting up Cloud KMS..."
if ! gcloud kms keyrings describe "$KEYRING" --location="$REGION" --project="$PROJECT_ID" &>/dev/null; then
  echo "--> Creating KMS Keyring: $KEYRING..."
  gcloud kms keyrings create "$KEYRING" --location="$REGION" --project="$PROJECT_ID"
else
  echo "--> KMS Keyring $KEYRING already exists."
fi

if ! gcloud kms keys describe "$KEY" --keyring="$KEYRING" --location="$REGION" --project="$PROJECT_ID" &>/dev/null; then
  echo "--> Creating KMS CryptoKey: $KEY..."
  gcloud kms keys create "$KEY" \
    --location="$REGION" \
    --keyring="$KEYRING" \
    --purpose="encryption" \
    --project="$PROJECT_ID"
else
  echo "--> KMS CryptoKey $KEY already exists."
fi

KMS_KEY_RESOURCE="projects/$PROJECT_ID/locations/$REGION/keyRings/$KEYRING/cryptoKeys/$KEY"
echo "KMS Key Resource Name: $KMS_KEY_RESOURCE"

# 3. Retrieve Cloud Run Service Account
echo "==> Retrieving Cloud Run service account..."
RUN_SA=$(gcloud run services describe "$RUN_SERVICE" \
  --region="$REGION" \
  --project="$PROJECT_ID" \
  --format="value(spec.template.spec.serviceAccountName)")

if [[ -z "$RUN_SA" ]]; then
  echo "Error: Could not retrieve service account for Cloud Run service $RUN_SERVICE" >&2
  exit 1
fi
echo "Cloud Run Service Account: $RUN_SA"

# 4. Grant Cloud Run Service Account permission to encrypt/decrypt using the KMS Key
echo "==> Granting KMS Decrypter/Encrypter role to Cloud Run Service Account..."
gcloud kms keys add-iam-policy-binding "$KEY" \
  --location="$REGION" \
  --keyring="$KEYRING" \
  --member="serviceAccount:$RUN_SA" \
  --role="roles/cloudkms.cryptoKeyEncrypterDecrypter" \
  --project="$PROJECT_ID" \
  --quiet

# 5. Grant Cloud Run Service Account Cloud Build Editor permissions (for builds dispatch)
echo "==> Granting Cloud Build Editor role to Cloud Run Service Account..."
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$RUN_SA" \
  --role="roles/cloudbuild.builds.editor" \
  --quiet

# 6. Grant Cloud Run Service Account Service Account User permission on the default Cloud Build Service Account (Skipped: legacy account does not exist; builds use default compute service account)
# echo "==> Retrieving project number..."
# PROJECT_NUM=$(gcloud projects describe "$PROJECT_ID" --format="value(projectNumber)")
# CB_SA="$PROJECT_NUM@cloudbuild.gserviceaccount.com"
# 
# echo "==> Granting Service Account User role to Cloud Run Service Account on Cloud Build service account..."
# gcloud iam service-accounts add-iam-policy-binding "$CB_SA" \
#   --member="serviceAccount:$RUN_SA" \
#   --role="roles/iam.serviceAccountUser" \
#   --project="$PROJECT_ID" \
#   --quiet

# 7. Create Service Account for Cloud Workflows
WORKFLOW_SA_NAME="gitbucket-workflows-sa"
WORKFLOW_SA="$WORKFLOW_SA_NAME@$PROJECT_ID.iam.gserviceaccount.com"

echo "==> Creating Workflows service account..."
if ! gcloud iam service-accounts describe "$WORKFLOW_SA" --project="$PROJECT_ID" &>/dev/null; then
  gcloud iam service-accounts create "$WORKFLOW_SA_NAME" \
    --display-name="GitBucket Workflows Service Account" \
    --project="$PROJECT_ID"
else
  echo "--> Workflows service account already exists."
fi

# 8. Grant Workflows Service Account permission to invoke the Cloud Run service
echo "==> Granting Cloud Run Invoker role to Workflows Service Account..."
gcloud run services add-iam-policy-binding "$RUN_SERVICE" \
  --region="$REGION" \
  --member="serviceAccount:$WORKFLOW_SA" \
  --role="roles/run.invoker" \
  --project="$PROJECT_ID" \
  --quiet

# 9. Wait for service account replication
echo "==> Waiting 15 seconds for service account IAM replication..."
sleep 15

# 10. Deploy Cloud Workflows definition
echo "==> Deploying Cloud Workflows workflow..."
gcloud workflows deploy "$WORKFLOW_NAME" \
  --source=sync-workflow.yaml \
  --location="$REGION" \
  --service-account="$WORKFLOW_SA" \
  --project="$PROJECT_ID"

echo "==> Infrastructure setup completed successfully!"
