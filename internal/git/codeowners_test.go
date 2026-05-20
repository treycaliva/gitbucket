package git

import (
	"strings"
	"testing"
)

func TestCodeOwnersBasic(t *testing.T) {
	co, err := ParseCodeOwners(strings.NewReader(`
# Comment
*           @alice
*.go        @bob
/docs/      @carol
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		path string
		want []string
	}{
		{"README.md", []string{"@alice"}},
		{"main.go", []string{"@bob"}},
		{"docs/intro.md", []string{"@carol"}},
		{"src/foo.go", []string{"@bob"}}, // last matching wins
	}
	for _, tt := range tests {
		got := co.Match(tt.path)
		if !equalSlice(got, tt.want) {
			t.Errorf("Match(%q)=%v want %v", tt.path, got, tt.want)
		}
	}
}

func TestCodeOwnersLastMatchWins(t *testing.T) {
	co, _ := ParseCodeOwners(strings.NewReader(`
*       @everyone
*.go    @gopher
`))
	got := co.Match("main.go")
	if len(got) != 1 || got[0] != "@gopher" {
		t.Fatalf("got %v", got)
	}
}

func TestCodeOwnersDoubleStar(t *testing.T) {
	co, _ := ParseCodeOwners(strings.NewReader(`/internal/** @backend`))
	got := co.Match("internal/auth/repo_access.go")
	if len(got) != 1 || got[0] != "@backend" {
		t.Fatalf("got %v", got)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
