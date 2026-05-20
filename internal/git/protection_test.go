package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// run executes git in the given dir; failing the test on error.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitOrSkip checks `git` is on $PATH; skips otherwise so CI without git won't fail.
func gitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available; skipping repo-backed tests")
	}
}

// makeRepo initializes a bare repo plus a sibling work-tree and returns (barePath, workPath).
// The work-tree is wired to push into the bare via "origin", and starts on a "main" branch
// so test expectations don't depend on the local git init.defaultBranch.
func makeRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "repo.git")
	work := filepath.Join(dir, "work")
	run(t, dir, "init", "--bare", bare)
	run(t, dir, "init", work)
	run(t, work, "remote", "add", "origin", bare)
	run(t, work, "checkout", "-b", "main")
	return bare, work
}

func TestEnforcePushBlockForcePush(t *testing.T) {
	rules := []Rule{{Pattern: "main", BlockForcePush: true}}
	updates := []RefUpdate{{RefName: "refs/heads/main", OldSha: "abc", NewSha: "def", IsForce: true}}
	res := EnforcePush(rules, updates, "u1")
	if len(res.Rejected) != 1 {
		t.Fatalf("expected 1 reject, got %d", len(res.Rejected))
	}
}

func TestEnforcePushBlockDeletion(t *testing.T) {
	rules := []Rule{{Pattern: "main", BlockDeletion: true}}
	updates := []RefUpdate{{RefName: "refs/heads/main", OldSha: "abc", NewSha: "0000000000000000000000000000000000000000", IsDelete: true}}
	res := EnforcePush(rules, updates, "u1")
	if len(res.Rejected) != 1 {
		t.Fatal("expected 1 reject")
	}
}

func TestEnforcePushAllowlist(t *testing.T) {
	rules := []Rule{{Pattern: "main", PushAllowlist: []string{"u-good"}}}
	res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-good")
	if len(res.Rejected) != 0 {
		t.Fatal("u-good should be allowed")
	}
	res = EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-bad")
	if len(res.Rejected) != 1 {
		t.Fatal("u-bad should be rejected")
	}
}

func TestEnforcePushEmptyAllowlistMeansNoDirectPush(t *testing.T) {
	rules := []Rule{{Pattern: "main", PushAllowlist: []string{}}}
	res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-anybody")
	if len(res.Rejected) != 1 {
		t.Fatal("empty allowlist must reject all")
	}
}

func TestEnforcePushNonMatchingRefPasses(t *testing.T) {
	rules := []Rule{{Pattern: "main", BlockForcePush: true}}
	res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/feature/x", OldSha: "a", NewSha: "b", IsForce: true}}, "u")
	if len(res.Rejected) != 0 {
		t.Fatal("feature branch should not match main rule")
	}
}

func TestMatchRuleMostSpecific(t *testing.T) {
	rules := []Rule{{Pattern: "*"}, {Pattern: "release/*"}, {Pattern: "release/v1"}}
	m := MatchRule(rules, "release/v1")
	if m == nil || m.Pattern != "release/v1" {
		t.Fatalf("got %+v", m)
	}
}

func TestIsAncestor(t *testing.T) {
	gitOrSkip(t)
	bare, work := makeRepo(t)

	// commit A on main
	if err := os.WriteFile(filepath.Join(work, "f"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "f")
	run(t, work, "commit", "-m", "a")
	shaA := run(t, work, "rev-parse", "HEAD")
	run(t, work, "push", "origin", "main")

	// commit B as descendant of A
	if err := os.WriteFile(filepath.Join(work, "f"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "f")
	run(t, work, "commit", "-m", "b")
	shaB := run(t, work, "rev-parse", "HEAD")
	run(t, work, "push", "origin", "main")

	// divergent branch C from A
	run(t, work, "checkout", "-b", "alt", shaA)
	if err := os.WriteFile(filepath.Join(work, "f"), []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "f")
	run(t, work, "commit", "-m", "c")
	shaC := run(t, work, "rev-parse", "HEAD")
	run(t, work, "push", "origin", "alt")

	if !IsAncestor(bare, shaA, shaB) {
		t.Fatalf("A should be ancestor of B")
	}
	if IsAncestor(bare, shaB, shaC) {
		t.Fatalf("B should NOT be ancestor of C (divergent)")
	}
	if !IsAncestor(bare, "", shaB) {
		t.Fatalf("empty oldSha should be treated as ancestor")
	}
	if !IsAncestor(bare, zeroSha, shaB) {
		t.Fatalf("zero-SHA oldSha should be treated as ancestor")
	}
}

func TestDiffRefsDetectsForceAndDelete(t *testing.T) {
	gitOrSkip(t)
	bare, work := makeRepo(t)

	// commit A on main
	if err := os.WriteFile(filepath.Join(work, "f"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "f")
	run(t, work, "commit", "-m", "a")
	shaA := run(t, work, "rev-parse", "HEAD")
	run(t, work, "push", "origin", "main")

	pre := LocalRefs(bare)
	if pre["refs/heads/main"] != shaA {
		t.Fatalf("pre refs mismatch: %+v", pre)
	}

	// reset hard to a parentless divergent commit, then force-push.
	run(t, work, "reset", "--hard", shaA)
	run(t, work, "commit", "--amend", "-m", "divergent")
	shaC := run(t, work, "rev-parse", "HEAD")
	run(t, work, "push", "--force", "origin", "main")

	post := LocalRefs(bare)
	if post["refs/heads/main"] != shaC {
		t.Fatalf("post refs mismatch: %+v", post)
	}
	updates := DiffRefs(bare, pre, post)
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d (%+v)", len(updates), updates)
	}
	u := updates[0]
	if u.RefName != "refs/heads/main" || !u.IsForce || u.IsDelete {
		t.Fatalf("expected force update on main, got %+v", u)
	}

	// Simulate deletion: post-state without main.
	postDelete := map[string]string{}
	updates = DiffRefs(bare, post, postDelete)
	if len(updates) != 1 || !updates[0].IsDelete {
		t.Fatalf("expected one deletion, got %+v", updates)
	}
}

func TestDiffRefsNewBranch(t *testing.T) {
	gitOrSkip(t)
	bare, work := makeRepo(t)
	if err := os.WriteFile(filepath.Join(work, "f"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "f")
	run(t, work, "commit", "-m", "a")
	run(t, work, "push", "origin", "main")

	pre := LocalRefs(bare)

	run(t, work, "checkout", "-b", "feature/x")
	if err := os.WriteFile(filepath.Join(work, "f"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "commit", "-am", "b")
	run(t, work, "push", "origin", "feature/x")

	post := LocalRefs(bare)
	updates := DiffRefs(bare, pre, post)
	if len(updates) != 1 {
		t.Fatalf("expected 1 new-branch update, got %d (%+v)", len(updates), updates)
	}
	if updates[0].RefName != "refs/heads/feature/x" || updates[0].IsForce || updates[0].IsDelete || updates[0].OldSha != "" {
		t.Fatalf("expected new-branch update, got %+v", updates[0])
	}
}
