package repocache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeRepo creates <root>/<owner>/<repo>.git with a file of the given size and
// backdates its access time so it isn't shielded by the grace period.
func makeRepo(t *testing.T, root, owner, repo string, size int) {
	t.Helper()
	dir := filepath.Join(root, owner, repo+".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(root, owner, repo string) bool {
	_, err := os.Stat(filepath.Join(root, owner, repo+".git"))
	return err == nil
}

// allowGuard is a LockGuard that always succeeds, standing in for the Firestore
// lock in unit tests.
func allowGuard(string, string) (func(), bool) { return func() {}, true }

func TestSweepEvictsColdestFirst(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "alice", "old", 1000)
	makeRepo(t, root, "alice", "new", 1000)

	// Budget fits one repo. Both are old enough to evict; "new" is warmer.
	m := New(root, 1500)
	m.grace = 0
	past := time.Now().Add(-time.Hour)
	m.access["alice/old"] = past
	m.access["alice/new"] = past.Add(time.Minute) // more recently used

	freed := m.Sweep(context.Background(), allowGuard)
	if freed < 1000 {
		t.Fatalf("freed = %d, want >= 1000", freed)
	}
	if exists(root, "alice", "old") {
		t.Error("coldest repo 'old' should have been evicted")
	}
	if !exists(root, "alice", "new") {
		t.Error("warmer repo 'new' should have survived")
	}
}

func TestSweepUnderBudgetIsNoop(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "bob", "a", 500)
	m := New(root, 1<<20)
	if freed := m.Sweep(context.Background(), allowGuard); freed != 0 {
		t.Fatalf("freed = %d, want 0 when under budget", freed)
	}
	if !exists(root, "bob", "a") {
		t.Error("repo should not be evicted under budget")
	}
}

func TestSweepSkipsPinned(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "carol", "hot", 2000)
	makeRepo(t, root, "carol", "cold", 2000)

	m := New(root, 1000) // over budget; must evict something
	m.grace = 0
	m.access["carol/hot"] = time.Now().Add(-time.Hour)
	m.access["carol/cold"] = time.Now().Add(-2 * time.Hour) // colder
	m.Pin("carol", "cold")                                  // ...but pinned, so untouchable

	m.Sweep(context.Background(), allowGuard)

	if !exists(root, "carol", "cold") {
		t.Error("pinned repo must never be evicted even when coldest")
	}
	if exists(root, "carol", "hot") {
		t.Error("unpinned repo should have been evicted instead")
	}
}

func TestSweepRespectsGrace(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "dan", "fresh", 5000)
	m := New(root, 100) // way over budget
	// Default 5-minute grace; a just-accessed repo is shielded.
	m.access["dan/fresh"] = time.Now()
	m.Sweep(context.Background(), allowGuard)
	if !exists(root, "dan", "fresh") {
		t.Error("repo accessed within the grace period should not be evicted")
	}
}

func TestSweepSkipsWhenGuardDenies(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "erin", "locked", 5000)
	m := New(root, 100)
	m.grace = 0
	m.access["erin/locked"] = time.Now().Add(-time.Hour)

	denyGuard := func(string, string) (func(), bool) { return nil, false }
	if freed := m.Sweep(context.Background(), denyGuard); freed != 0 {
		t.Fatalf("freed = %d, want 0 when guard denies", freed)
	}
	if !exists(root, "erin", "locked") {
		t.Error("repo whose lock could not be taken must not be evicted")
	}
}

func TestDisabledManagerIsNoop(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "frank", "a", 5000)

	var nilMgr *Manager
	if nilMgr.Enabled() {
		t.Error("nil Manager must report disabled")
	}
	nilMgr.Pin("frank", "a") // must not panic
	nilMgr.Touch("frank", "a")
	nilMgr.Unpin("frank", "a")
	if freed := nilMgr.Sweep(context.Background(), allowGuard); freed != 0 {
		t.Fatalf("nil Manager Sweep freed = %d, want 0", freed)
	}

	disabled := New(root, 0)
	if disabled.Enabled() {
		t.Error("maxBytes=0 must report disabled")
	}
	if freed := disabled.Sweep(context.Background(), allowGuard); freed != 0 {
		t.Fatalf("disabled Sweep freed = %d, want 0", freed)
	}
	if !exists(root, "frank", "a") {
		t.Error("disabled manager must never evict")
	}
}

func TestScanFallsBackToModTime(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "gwen", "leftover", 3000) // never Pinned/Touched in-process
	m := New(root, 100)
	m.grace = 0
	// Backdate the dir so the mtime fallback makes it evictable.
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(root, "gwen", "leftover.git"), old, old)

	m.Sweep(context.Background(), allowGuard)
	if exists(root, "gwen", "leftover") {
		t.Error("repo with no in-process access record should evict via mtime fallback")
	}
}
