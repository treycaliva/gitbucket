package apps

import "testing"

func TestEventTypeString(t *testing.T) {
	cases := []struct {
		t    EventType
		want string
	}{
		{EventPullRequest, "pull_request"},
		{EventPush, "push"},
		{EventInstallation, "installation"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("EventType(%d).String() = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestPullRequestPayloadShape(t *testing.T) {
	p := PullRequestPayload{
		Action:     "opened",
		Number:     7,
		Title:      "x",
		Body:       "y",
		State:      "open",
		HeadBranch: "feature",
		BaseBranch: "main",
		OwnerLogin: "alice",
		RepoName:   "demo",
		Sender:     SenderRef{Login: "bob", ID: 12345, Type: "User"},
	}
	if p.Action != "opened" {
		t.Errorf("Action = %q", p.Action)
	}
	if p.Sender.Type != "User" {
		t.Errorf("Sender.Type = %q", p.Sender.Type)
	}
}

func TestPushPayloadShape(t *testing.T) {
	p := PushPayload{
		OwnerLogin: "alice",
		RepoName:   "demo",
		Ref:        "refs/heads/main",
		Before:     "0000000000000000000000000000000000000000",
		After:      "abc123",
		Sender:     SenderRef{Login: "alice", ID: 1, Type: "User"},
	}
	if p.Ref != "refs/heads/main" {
		t.Errorf("Ref = %q", p.Ref)
	}
}
