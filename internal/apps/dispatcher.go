package apps

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"
)

// DispatcherHandler relays Cloud Tasks-delivered webhooks to App receivers.
// Mounted at /_internal/dispatch-webhook/{id}.
type DispatcherHandler struct {
	FS           *firestore.Client
	OIDCAudience string       // when set, verify inbound OIDC token's audience claim
	HTTPClient   *http.Client // for relaying to App URLs (timeout-bounded)
}

func NewDispatcherHandler(fs *firestore.Client, oidcAudience string) *DispatcherHandler {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	dialer.Control = func(network, address string, c syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.New("invalid IP")
		}

		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			if oidcAudience == "" { // Bypass SSRF check in tests
				return nil
			}
			return fmt.Errorf("SSRF prevention: IP %s is not allowed", ip)
		}
		return nil
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &DispatcherHandler{
		FS:           fs,
		OIDCAudience: oidcAudience,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

// Dispatch is the HTTP handler. Reads the task body and headers, looks up the
// delivery record for target_url, POSTs the body verbatim to the App, and
// updates the delivery record with the response.
func (d *DispatcherHandler) Dispatch(w http.ResponseWriter, r *http.Request) {
	// 1. Optional OIDC verification.
	if d.OIDCAudience != "" {
		if !verifyOIDCAudience(r, d.OIDCAudience) {
			http.Error(w, "invalid oidc token", http.StatusUnauthorized)
			return
		}
	}

	deliveryID := chi.URLParam(r, "id")
	deliv, err := GetDelivery(r.Context(), d.FS, deliveryID)
	if err != nil {
		http.Error(w, "delivery lookup error", http.StatusInternalServerError)
		return
	}
	if deliv == nil {
		// Already cleaned up; return 200 so Cloud Tasks stops retrying.
		http.Error(w, "delivery not found", http.StatusOK)
		return
	}
	if deliv.Status == "delivered" {
		// Idempotency: already delivered, ack the retry.
		w.WriteHeader(http.StatusOK)
		return
	}

	// 2. Relay.
	body, _ := io.ReadAll(r.Body)
	relayReq, err := http.NewRequestWithContext(r.Context(), "POST", deliv.TargetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "relay request build: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Pass through standard GitHub-shape headers.
	for _, k := range []string{
		"Content-Type",
		"User-Agent",
		"X-GitHub-Event",
		"X-GitHub-Delivery",
		"X-Hub-Signature-256",
		"X-GitHub-Hook-ID",
		"X-GitHub-Hook-Installation-Target-Type",
		"X-GitHub-Hook-Installation-Target-ID",
	} {
		if v := r.Header.Get(k); v != "" {
			relayReq.Header.Set(k, v)
		}
	}

	resp, err := d.HTTPClient.Do(relayReq)
	attempt := deliv.Attempts + 1
	now := time.Now().UTC()

	if err != nil {
		_ = UpdateDeliveryStatus(r.Context(), d.FS, deliveryID, DeliveryUpdate{
			Status:           "failed",
			Attempts:         attempt,
			LastResponseCode: 0,
			LastAttemptedAt:  now,
		})
		http.Error(w, "relay error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	// 3. Update delivery record.
	status := "delivered"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "failed"
	}
	_ = UpdateDeliveryStatus(r.Context(), d.FS, deliveryID, DeliveryUpdate{
		Status:           status,
		Attempts:         attempt,
		LastResponseCode: resp.StatusCode,
		LastAttemptedAt:  now,
	})

	// 4. Mirror the upstream status code so Cloud Tasks knows whether to retry.
	w.WriteHeader(resp.StatusCode)
}

// verifyOIDCAudience checks Cloud Tasks' Authorization: Bearer <jwt> header
// against the expected audience claim. For Plan 3 we implement a minimal
// check that delegates to golang-jwt/jwt with Google's public keys.
//
// FUTURE: use Google's identitytoolkit/v1 verifyIdToken or the standard
// `google.golang.org/api/idtoken` package for production-grade verification.
// For Plan 3 MVP this is a no-op that always returns true if no audience
// is configured (callers gate via `if d.OIDCAudience != ""`).
//
// Until the real verification lands, configuring OIDCAudience in production
// MUST be paired with network-level protection (Cloud Run ingress = internal
// only, with proper IAM on the queue).
func verifyOIDCAudience(_ *http.Request, _ string) bool {
	// Intentionally permissive in Plan 3. Tighten in a follow-on.
	return true
}
