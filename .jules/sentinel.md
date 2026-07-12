## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2026-05-24 - SSRF Vulnerability in Webhook Dispatcher
**Vulnerability:** The application was using the default `http.Client` to dispatch webhooks to user-provided URLs (`TargetURL`). This allowed users to point webhooks at internal IPs (e.g., `127.0.0.1`, private subnets, cloud metadata endpoints), resulting in Server-Side Request Forgery (SSRF) and potential DNS rebinding attacks.
**Learning:** Default HTTP clients in Go follow redirects and resolve DNS without restriction. Any outbound HTTP request to a user-provided URL must be strictly controlled to prevent accessing internal resources. Also, when parsing IP addresses, it is critical to fail closed if `net.ParseIP` returns `nil` (e.g., for IPv6 with zone index).
**Prevention:** Always use a custom `http.Client` with an SSRF-safe `net.Dialer`. The dialer's `Control` hook should inspect the resolved IP address and reject connections to loopback, private, and link-local ranges. Furthermore, use `http.DefaultTransport.(*http.Transport).Clone()` when creating custom transports to avoid dropping important defaults like proxy settings and HTTP/2 support.
