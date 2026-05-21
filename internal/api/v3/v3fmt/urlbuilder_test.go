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
		{b.UserAvatar("alice"), "https://gitbucket.example.com/avatars/alice"},
		{b.ContentsAPI("alice", "repo", "src/main.go", "main"), "https://gitbucket.example.com/api/v3/repos/alice/repo/contents/src/main.go?ref=main"},
		{b.GitRefAPI("alice", "repo", "heads/main"), "https://gitbucket.example.com/api/v3/repos/alice/repo/git/ref/heads/main"},
		{b.GitTreeAPI("alice", "repo", "abc123"), "https://gitbucket.example.com/api/v3/repos/alice/repo/git/trees/abc123"},
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

func TestContentsAPIEscapesRef(t *testing.T) {
	b := NewURLBuilder("https://gitbucket.example.com")
	got := b.ContentsAPI("o", "r", "f.go", "feature/x?y=1&z=2")
	want := "https://gitbucket.example.com/api/v3/repos/o/r/contents/f.go?ref=feature%2Fx%3Fy%3D1%26z%3D2"
	if got != want {
		t.Errorf("ContentsAPI escape:\n got %q\nwant %q", got, want)
	}
}
