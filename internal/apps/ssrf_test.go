package apps

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestSafeHTTPClient_BlocksInternal(t *testing.T) {
	client := SafeHTTPClient()

	tests := []struct {
		url     string
		blocked bool
	}{
		{"http://example.com", false},
		{"http://169.254.169.254", true},
		{"http://127.0.0.1", true},
		{"http://localhost", true},
		{"http://[::1]", true},
		{"http://10.0.0.1", true},
		{"http://192.168.1.1", true},
		{"http://metadata.google.internal", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(context.Background(), "GET", tt.url, nil)
			_, err := client.Do(req)

			if tt.blocked && err == nil {
				t.Errorf("Expected request to %s to be blocked by SSRF protection, but it succeeded", tt.url)
			}

			if !tt.blocked && err != nil && err.Error() != "" {
				// It's ok if it fails for other reasons (e.g. no network),
				// but it shouldn't fail with our SSRF error
				if containsSSRFError(err) {
					t.Errorf("Request to %s blocked incorrectly by SSRF protection: %v", tt.url, err)
				}
			}
		})
	}
}

func containsSSRFError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, str := range []string{"SSRF protection", "blocked"} {
		if !strings.Contains(msg, str) {
			return false
		}
	}
	return true
}
