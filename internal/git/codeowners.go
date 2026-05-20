package git

import (
	"bufio"
	"io"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// CodeOwnersRule represents a single rule line in a CODEOWNERS file.
type CodeOwnersRule struct {
	Pattern string
	Owners  []string
	LineNo  int
}

// CodeOwners is a parsed CODEOWNERS file.
type CodeOwners struct {
	Rules []CodeOwnersRule
}

// ParseCodeOwners reads a CODEOWNERS file from r and returns the parsed rules.
// Comments (# ...) and blank lines are skipped. The first whitespace-separated
// field is the pattern; subsequent fields are owners (handles or emails).
func ParseCodeOwners(r io.Reader) (*CodeOwners, error) {
	co := &CodeOwners{}
	s := bufio.NewScanner(r)
	line := 0
	for s.Scan() {
		line++
		raw := strings.TrimSpace(s.Text())
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		if raw == "" {
			continue
		}
		fields := strings.Fields(raw)
		if len(fields) < 2 {
			continue
		}
		co.Rules = append(co.Rules, CodeOwnersRule{Pattern: fields[0], Owners: fields[1:], LineNo: line})
	}
	return co, s.Err()
}

// Match returns the owners for the given path. Last-matching rule wins.
// Returns nil if no rule matches.
func (c *CodeOwners) Match(p string) []string {
	p = strings.TrimPrefix(path.Clean(p), "/")
	var winners []string
	for _, r := range c.Rules {
		if codeownerMatch(r.Pattern, p) {
			winners = r.Owners
		}
	}
	return winners
}

// codeownerMatch implements a gitignore/CODEOWNERS-style pattern match using
// the doublestar library for glob support.
//
// Rules:
//   - A leading "/" anchors the pattern to the repo root.
//   - A trailing "/" makes the pattern match a directory (we treat this as
//     matching that directory or any file within it).
//   - "**" matches across path separators.
//   - Patterns without "/" (and not anchored) match any file/dir with that
//     basename anywhere in the tree.
func codeownerMatch(pattern, p string) bool {
	pat := pattern
	anchored := strings.HasPrefix(pat, "/")
	pat = strings.TrimPrefix(pat, "/")
	dirOnly := strings.HasSuffix(pat, "/")
	pat = strings.TrimSuffix(pat, "/")

	// Basename match: pattern without "/" and not anchored matches anywhere.
	if !anchored && !strings.Contains(pat, "/") {
		base := path.Base(p)
		if ok, _ := doublestar.Match(pat, base); ok {
			return true
		}
		// Fall through to whole-path match too.
	}

	// Directory-only patterns: match the directory itself or anything beneath it.
	if dirOnly {
		if ok, _ := doublestar.Match(pat, p); ok {
			return true
		}
		if ok, _ := doublestar.Match(pat+"/**", p); ok {
			return true
		}
		return false
	}

	if ok, _ := doublestar.Match(pat, p); ok {
		return true
	}
	return false
}
