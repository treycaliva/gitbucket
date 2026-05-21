package v3fmt

import "testing"

func TestURLBuilder(t *testing.T) {
	b := NewURLBuilder("https://gitbucket.example.com")

	cases := []struct {
		got, want string
	}{
		{b.APIRoot(), "https://gitbucket.example.com/api/v3"},
		{b.UserHTML("alice"), "https://gitbucket.example.com/alice"},
		{b.UserAPI("alice"), "https://gitbucket.example.com/api/v3/users/alice"},
		{b.RepoHTML("alice", "repo"), "https://gitbucket.example.com/alice/repo"},
		{b.RepoAPI("alice", "repo"), "https://gitbucket.example.com/api/v3/repos/alice/repo"},
		{b.PullAPI("alice", "repo", 42), "https://gitbucket.example.com/api/v3/repos/alice/repo/pulls/42"},
		{b.PullHTML("alice", "repo", 42), "https://gitbucket.example.com/alice/repo/pulls/42"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

func TestURLBuilderTrimsTrailingSlash(t *testing.T) {
	b := NewURLBuilder("https://gitbucket.example.com/")
	if got := b.APIRoot(); got != "https://gitbucket.example.com/api/v3" {
		t.Errorf("APIRoot with trailing slash: %q", got)
	}
}
