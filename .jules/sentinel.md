## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2026-06-20 - Server-Side Request Forgery (SSRF) in Webhooks
**Vulnerability:** The application was making outbound HTTP POST requests to user-provided webhook URLs (`target_url`) using the default `http.Client`. This allows attackers to specify internal IPs or `localhost`, causing the server to make requests to internal services on their behalf (Server-Side Request Forgery).
**Learning:** Default HTTP clients follow redirects and resolve DNS without restriction. An attacker can set a webhook URL to `http://169.254.169.254` (cloud metadata service) or use a custom domain that resolves to a private IP (DNS rebinding attack). Go's standard library provides a powerful `Control` hook in `net.Dialer` that executes *after* DNS resolution but *before* the connection is established.
**Prevention:** When making outbound HTTP requests to user-provided URLs, always use a custom `http.Client` with a customized `http.Transport` and `net.Dialer`. Implement the `Control` hook in the dialer to parse the resolved IP address and explicitly block loopback, private, and link-local unicast addresses (`ip.IsLoopback()`, `ip.IsPrivate()`, `ip.IsLinkLocalUnicast()`). This defends against both direct IP payloads and DNS rebinding attacks.
