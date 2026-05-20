package main

import (
	"net/http/httptest"
	"testing"
)

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{
			name:       "no XFF falls back to RemoteAddr host",
			remoteAddr: "203.0.113.5:54321",
			want:       "203.0.113.5",
		},
		{
			name:       "no XFF and RemoteAddr without port returns as-is",
			remoteAddr: "203.0.113.5",
			want:       "203.0.113.5",
		},
		{
			name:       "single XFF entry returned",
			xff:        "198.51.100.7",
			remoteAddr: "10.0.0.1:443",
			want:       "198.51.100.7",
		},
		{
			name:       "multi-hop XFF returns leftmost (original client) entry",
			xff:        "198.51.100.7, 203.0.113.9, 10.0.0.1",
			remoteAddr: "127.0.0.1:443",
			want:       "198.51.100.7",
		},
		{
			name:       "XFF entries are trimmed",
			xff:        "   198.51.100.7  , 203.0.113.9",
			remoteAddr: "127.0.0.1:443",
			want:       "198.51.100.7",
		},
		{
			name:       "IPv6 RemoteAddr without XFF",
			remoteAddr: "[2001:db8::1]:443",
			want:       "2001:db8::1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			got := getClientIP(r)
			if got != tc.want {
				t.Errorf("getClientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
