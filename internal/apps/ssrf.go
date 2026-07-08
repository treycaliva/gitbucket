package apps

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// SafeHTTPClient creates an http.Client that prevents Server-Side Request Forgery (SSRF)
// by refusing to connect to private, loopback, or otherwise internal IP addresses.
// It uses a custom Dialer Control function to prevent DNS rebinding attacks.
func SafeHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip != nil {
				if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
					return fmt.Errorf("SSRF protection: IP %s is blocked", ip.String())
				}
				if ip.To4() != nil && ip.To4()[0] == 169 && ip.To4()[1] == 254 {
					return fmt.Errorf("SSRF protection: metadata IP %s is blocked", ip.String())
				}
			}
			return nil
		},
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			// Block metadata server explicitly even if DNS fails
			if host == "metadata.google.internal" || host == "metadata" || host == "localhost" {
				return nil, errors.New("SSRF protection: access to internal hostnames is blocked")
			}

			// Do an initial DNS lookup just to fail fast, actual IP dialed will be
			// checked by the Control func above to prevent DNS Rebinding (TOCTOU).
			// Use context-aware lookup to prevent DNS hang attacks.
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}

			for _, ipAddr := range ips {
				ip := ipAddr.IP
				if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
					return nil, fmt.Errorf("SSRF protection: IP %s is blocked", ip.String())
				}
				if ip.To4() != nil && ip.To4()[0] == 169 && ip.To4()[1] == 254 {
					return nil, fmt.Errorf("SSRF protection: metadata IP %s is blocked", ip.String())
				}
			}

			// Dialing with the original addr string. The standard net library will resolve
			// it again, but before connecting, our `Control` function gets the actual IP
			// being dialed and verifies it, mitigating TOCTOU/DNS Rebinding.
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}
