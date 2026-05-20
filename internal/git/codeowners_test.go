package git

import (
	"os"
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

func TestLoadCodeOwnersResolutionOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/docs", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dir+"/docs/CODEOWNERS", []byte("* @docs"), 0o644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	co, err := LoadCodeOwners(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := co.Match("README.md")
	if len(got) != 1 || got[0] != "@docs" {
		t.Fatalf("expected docs/CODEOWNERS to be used, got %v", got)
	}

	// Add a root CODEOWNERS — it must win over docs/CODEOWNERS.
	if err := os.WriteFile(dir+"/CODEOWNERS", []byte("* @root"), 0o644); err != nil {
		t.Fatalf("write root: %v", err)
	}
	co, err = LoadCodeOwners(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got = co.Match("README.md")
	if len(got) != 1 || got[0] != "@root" {
		t.Fatalf("root must win over docs, got %v", got)
	}

	// Add a .gitbucket/CODEOWNERS — root still wins.
	if err := os.MkdirAll(dir+"/.gitbucket", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dir+"/.gitbucket/CODEOWNERS", []byte("* @gitbucket"), 0o644); err != nil {
		t.Fatalf("write gitbucket: %v", err)
	}
	co, _ = LoadCodeOwners(dir)
	got = co.Match("README.md")
	if len(got) != 1 || got[0] != "@root" {
		t.Fatalf("root must still win, got %v", got)
	}
}

func TestLoadCodeOwnersMissing(t *testing.T) {
	dir := t.TempDir()
	co, err := LoadCodeOwners(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if co == nil {
		t.Fatal("expected non-nil CodeOwners")
	}
	if got := co.Match("foo.go"); got != nil {
		t.Fatalf("expected nil owners for missing file, got %v", got)
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
