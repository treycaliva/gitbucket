## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2026-05-25 - SSRF via Outbound Webhooks
**Vulnerability:** The webhook dispatcher in `internal/apps/dispatcher.go` used a default `http.Client` to relay Cloud Tasks to user-provided webhook URLs. This allowed attackers to specify internal network boundaries (like localhost, private IP ranges, or metadata services) as their webhook target, leading to a Server-Side Request Forgery (SSRF) and DNS rebinding vulnerability.
**Learning:** Default HTTP clients in Go follow redirects and do not validate resolved IP addresses. When proxying or relaying requests to user-controlled URLs, the application's network perimeter must be defended at the socket level.
**Prevention:** For any outbound requests to untrusted URLs, construct a custom `http.Client` by cloning `http.DefaultTransport` and injecting a custom `net.Dialer`. The dialer's `Control` hook must inspect the parsed IP address (post-DNS resolution) and reject connections to loopback (`IsLoopback()`), private (`IsPrivate()`), link-local (`IsLinkLocalUnicast()`, `IsLinkLocalMulticast()`), and unspecified (`IsUnspecified()`) addresses. Additionally, enforce a fail-closed policy if IP parsing fails.
