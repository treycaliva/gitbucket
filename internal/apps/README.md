# Apps Package — Operational Notes

## One-time Firestore TTL setup (per environment)

Installation tokens carry an `expires_at` field; Firestore native TTL deletes
expired docs on a periodic sweep. Configure once per project:

```
gcloud firestore fields ttls update expires_at \
    --collection-group=installation_tokens \
    --enable-ttl
```

The middleware (`RequireInstallationToken`) ALSO checks `expires_at` against
now on every read — the TTL is cleanup, not enforcement. So a missed TTL sweep
does not create a security issue, only storage cruft.
