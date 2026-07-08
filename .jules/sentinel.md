## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2025-05-24 - SSRF Vulnerability in Webhook Dispatcher
**Vulnerability:** The application was using a standard `http.Client` without IP restrictions when dispatching webhooks to user-provided URLs (`TargetURL`). This allowed Server-Side Request Forgery (SSRF) where attackers could use the webhook dispatcher to perform unauthorized HTTP requests to internal networks (e.g., `127.0.0.1`, `10.0.0.x`) or cloud metadata endpoints (e.g., `169.254.169.254`, `metadata.google.internal`).
**Learning:** Whenever an application makes an HTTP request to a user-provided URL, the host and IP address must be rigorously validated to ensure they don't resolve to private, loopback, or cloud-specific internal IPs. Standard HTTP clients implicitly trust internal network routes.
**Prevention:** Always construct a custom `http.Client` with a patched `DialContext` function that performs DNS resolution and checks the resulting IP addresses against a blocklist of private (`IsPrivate()`), loopback (`IsLoopback()`), and link-local (`IsLinkLocalUnicast()`) address ranges before allowing the connection to proceed.
