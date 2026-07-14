## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2026-05-24 - SSRF via Outbound Webhook Relay
**Vulnerability:** The webhook dispatcher used `http.Client` without enforcing IP validation on resolved hostnames, leaving it vulnerable to Server-Side Request Forgery (SSRF). Attackers could have supplied webhooks pointing to internal network IP addresses (like `169.254.169.254` for cloud metadata) or used DNS rebinding.
**Learning:** Using default HTTP clients for user-supplied URLs is dangerous, as standard DNS resolution does not block private or local loopback addresses. Go's `net.Dialer.Control` hook allows for inspecting the exact IP resolved right before the connection is established. This ensures that any Time-Of-Check to Time-Of-Use (TOCTOU) issues via DNS rebinding are inherently blocked. Also, remember that `0.0.0.0` or `::` (unspecified IP) routes to localhost on Linux and must be blocked.
**Prevention:** For any HTTP client that accesses external or user-provided URLs, override the transport's `DialContext` with a custom `net.Dialer`. Implement a `Control` hook that parses the resolved IP and blocks it if it matches loopback, private, link-local, or unspecified ranges, and fail closed if the IP is unparseable.
