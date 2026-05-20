#!/usr/bin/env bash
set -euo pipefail

: "${GCS_BUCKET:?GCS_BUCKET must be set, e.g. git-bucket-repositories-79382}"

echo "Enabling 7-day soft-delete on gs://${GCS_BUCKET} ..."
gcloud storage buckets update "gs://${GCS_BUCKET}" --soft-delete-duration=7d

echo "Verifying ..."
gcloud storage buckets describe "gs://${GCS_BUCKET}" --format="value(softDeletePolicy.retentionDurationSeconds)"
