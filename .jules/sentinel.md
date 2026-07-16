## 2026-05-24 - IP Spoofing in GCP/Cloud Run via X-Forwarded-For
**Vulnerability:** The application was extracting the first (leftmost) IP address from the `X-Forwarded-For` header to determine the client's IP.
**Learning:** In Google Cloud Platform (specifically Cloud Run), the Google Front End (GFE) appends the actual connecting client IP to the *end* (rightmost) of the `X-Forwarded-For` header. Since clients can arbitrarily spoof initial values in this header, using the first element allows attackers to bypass IP gating mechanisms.
**Prevention:** Always extract the last (rightmost) element of the `X-Forwarded-For` header when running in GCP/Cloud Run to ensure the IP being checked is the one reliably appended by the Google infrastructure, not user-controlled input.

### Fixed Overly Permissive CORS Policy Vulnerability
- Dynamic `Origin` echoing combined with `Access-Control-Allow-Credentials: true` allows any site to perform authenticated cross-origin requests, leading to potential data leakage.
- For APIs that do not rely on cookies (like GitBucket, which uses `Authorization` bearer tokens), it is safer to use `Access-Control-Allow-Origin: *` and completely omit the `Access-Control-Allow-Credentials` header.
- This neutralizes cross-origin attacks where malicious sites might trick a user's browser into sending authenticated requests.

## 2026-06-15 - SSRF Vulnerability in Webhook Dispatcher
**Vulnerability:** The application was using the default `http.Client` for delivering outbound webhooks to user-provided URLs in `internal/apps/dispatcher.go`. This exposed the server to Server-Side Request Forgery (SSRF) and DNS rebinding attacks, allowing users to send arbitrary HTTP requests to internal IP addresses (e.g., `127.0.0.1`, `169.254.169.254`).
**Learning:** Default HTTP clients implicitly trust any resolved IP address, and simply validating the URL string beforehand is insufficient due to DNS rebinding.
**Prevention:** Use a custom `http.Client` with a modified `net.Dialer` whose `Control` hook uses `net.ParseIP` to explicitly check the resolved IP against loopback, private, link-local, and unspecified ranges (and strictly checking `ip == nil`) before establishing the connection. Also ensure local `httptest` servers correctly override this strict client with a standard one in unit tests.
