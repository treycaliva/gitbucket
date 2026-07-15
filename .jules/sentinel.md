## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2024-05-28 - SSRF Vulnerability in Webhook Dispatcher
**Vulnerability:** The application was making outbound HTTP requests to user-provided webhook URLs using the default `http.Client` without restricting the destination IP addresses.
**Learning:** This exposes the application to Server-Side Request Forgery (SSRF) and DNS rebinding attacks, as a malicious user could provide a webhook URL pointing to an internal IP address (like 127.0.0.1 or a private network IP) to access internal services or bypass firewalls.
**Prevention:** Always use a custom `http.Client` with an SSRF-safe `net.Dialer` that validates resolved IPs within the `Control` hook (checking against loopback, private, link-local, and unspecified ranges). Clone the default transport to retain critical configurations.
