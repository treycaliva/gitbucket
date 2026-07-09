## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2026-07-08 - SSRF Vulnerability in Webhook Dispatcher
**Vulnerability:** The `NewDispatcherHandler` in `internal/apps/dispatcher.go` used the default `http.Client` for sending webhooks without a custom dialer to restrict target IP addresses.
**Learning:** Outbound webhook dispatchers run the risk of SSRF (Server-Side Request Forgery) if a user configures a webhook URL that resolves to internal or sensitive private IPs (e.g., `127.0.0.1`, `169.254.169.254` GCP metadata server, or internal subnets).
**Prevention:** When making outbound HTTP requests to user-provided URLs in Go, always use a custom `http.Client` equipped with an SSRF-safe `net.Dialer`. The dialer's `Control` hook should resolve the target hostname and explicitly reject connections to loopback (`ip.IsLoopback()`), private (`ip.IsPrivate()`), and link-local (`ip.IsLinkLocalUnicast()`) IP ranges.
