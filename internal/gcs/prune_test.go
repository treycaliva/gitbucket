package gcs

import (
	"os"
	"path/filepath"
	"testing"
)

// write creates a file (and parent dirs) with a tiny payload.
func write(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func present(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func keepSet(rels ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(rels))
	for _, r := range rels {
		m[filepath.FromSlash(r)] = struct{}{}
	}
	return m
}

func TestPruneToMirrorRemovesStaleRefs(t *testing.T) {
	root := t.TempDir()
	// GCS has main; local also has a stale force-deleted branch and an orphan object.
	write(t, root, "HEAD")
	write(t, root, "refs/heads/main")
	write(t, root, "refs/heads/deleted")  // resurrected branch — must go
	write(t, root, "objects/ab/deadbeef") // referenced, kept
	write(t, root, "objects/cd/orphaned") // not in GCS — must go
	write(t, root, "last_sync_timestamp") // local-only bookkeeping — kept
	write(t, root, "lfs/oid123")          // lfs never syncs — kept

	keep := keepSet("HEAD", "refs/heads/main", "objects/ab/deadbeef")
	if err := pruneToMirror(root, keep); err != nil {
		t.Fatalf("pruneToMirror: %v", err)
	}

	for _, rel := range []string{"HEAD", "refs/heads/main", "objects/ab/deadbeef", "last_sync_timestamp", "lfs/oid123"} {
		if !present(root, rel) {
			t.Errorf("%s should have been kept", rel)
		}
	}
	for _, rel := range []string{"refs/heads/deleted", "objects/cd/orphaned"} {
		if present(root, rel) {
			t.Errorf("%s should have been pruned", rel)
		}
	}
	// The now-empty objects/cd directory should be cleaned up.
	if present(root, "objects/cd") {
		t.Error("empty directory objects/cd should have been removed")
	}
	// objects/ab still has a file, so it must remain.
	if !present(root, "objects/ab") {
		t.Error("objects/ab still holds a kept file and must remain")
	}
}

func TestPruneToMirrorNoopWhenAllPresent(t *testing.T) {
	root := t.TempDir()
	write(t, root, "HEAD")
	write(t, root, "refs/heads/main")
	keep := keepSet("HEAD", "refs/heads/main")
	if err := pruneToMirror(root, keep); err != nil {
		t.Fatalf("pruneToMirror: %v", err)
	}
	if !present(root, "HEAD") || !present(root, "refs/heads/main") {
		t.Error("nothing should be removed when local matches GCS")
	}
}

func TestPruneToMirrorEmptyKeepClearsRepoButKeepsLocalOnly(t *testing.T) {
	root := t.TempDir()
	write(t, root, "refs/heads/main")
	write(t, root, "last_sync_timestamp")
	// Empty keep set (e.g. GCS lists only lfs/ objects, which are filtered out).
	if err := pruneToMirror(root, keepSet()); err != nil {
		t.Fatalf("pruneToMirror: %v", err)
	}
	if present(root, "refs/heads/main") {
		t.Error("ref absent from GCS should be pruned even when keep is empty")
	}
	if !present(root, "last_sync_timestamp") {
		t.Error("last_sync_timestamp must survive pruning")
	}
}
