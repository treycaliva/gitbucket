package v3fmt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRefFormatter(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := Ref(RefSource{
		Owner: "alice", Repo: "demo",
		Ref: "heads/main",
		SHA: "abc123def456abc123def456abc123def4567890",
	}, urls)

	if got.Ref != "refs/heads/main" {
		t.Errorf("Ref = %q, want refs/heads/main", got.Ref)
	}
	if got.URL != "https://gitbucket.example.com/api/v3/repos/alice/demo/git/ref/heads/main" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Object.Type != "commit" {
		t.Errorf("Object.Type = %q", got.Object.Type)
	}
	if got.Object.SHA != "abc123def456abc123def456abc123def4567890" {
		t.Errorf("Object.SHA = %q", got.Object.SHA)
	}
	if got.Object.URL != "https://gitbucket.example.com/api/v3/repos/alice/demo/git/commits/abc123def456abc123def456abc123def4567890" {
		t.Errorf("Object.URL = %q", got.Object.URL)
	}
	if !strings.HasPrefix(got.NodeID, "UmVm") {
		// base64 encoding of "Ref:..." starts with the bytes that encode as UmVm
		t.Errorf("NodeID = %q, want it to start with UmVm (base64 'Ref:')", got.NodeID)
	}

	// Make sure it round-trips through JSON cleanly.
	if _, err := json.Marshal(got); err != nil {
		t.Errorf("json.Marshal: %v", err)
	}
}
