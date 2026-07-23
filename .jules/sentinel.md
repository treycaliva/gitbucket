## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2025-02-18 - SSRF Vulnerability in Webhook Dispatcher
**Vulnerability:** The application used the default `http.Client` without custom protections to send webhooks to user-provided URLs in `internal/apps/dispatcher.go`.
**Learning:** This exposes the application to Server-Side Request Forgery (SSRF) and DNS rebinding attacks, where an attacker could construct URLs that resolve to internal IPs (e.g. cloud metadata servers like `169.254.169.254`, `localhost`, or private network endpoints) to bypass firewall restrictions and extract sensitive data.
**Prevention:** When making outbound HTTP requests to user-provided URLs, always configure the `http.Client` to use a custom `net.Dialer`. Implement an SSRF-safe validation in the dialer's `Control` hook to verify that the resolved IP address is not within loopback, private, link-local, or unspecified ranges. Additionally, bypass these restrictions explicitly in test files when interacting with local `httptest` servers.
## 2024-07-23 - [CRITICAL] Fixed Authentication Bypass in Dispatcher Webhook

**Vulnerability:** The OIDC validation function `verifyOIDCAudience` for webhook dispatching was stubbed out to unconditionally return `true`, completely bypassing authentication for the internal webhook dispatch endpoint (`/_internal/dispatch-webhook/{id}`).
**Learning:** During MVP or early phases of development, security checks like OIDC verification are sometimes intentionally skipped with placeholder functions, leaving high-risk endpoints vulnerable if deployed to production without the follow-up implementation.
**Prevention:** Track stubbed security controls strictly and enforce network-level controls (e.g., Cloud Run internal ingress + IAM) as defense-in-depth until application-level authentication is fully implemented using trusted standard libraries like `google.golang.org/api/idtoken`.
