package v3fmt

import "testing"

func TestTreeFormatter(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := Tree(TreeSource{
		Owner: "alice", Repo: "demo",
		SHA: "abc123",
		Entries: []TreeEntrySource{
			{Path: "README.md", Mode: "100644", Type: "blob", SHA: "blob-sha-1", Size: 25},
			{Path: "src", Mode: "040000", Type: "tree", SHA: "tree-sha-2"},
		},
	}, urls)

	if got.SHA != "abc123" {
		t.Errorf("SHA = %q", got.SHA)
	}
	if got.URL != "https://gitbucket.example.com/api/v3/repos/alice/demo/git/trees/abc123" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Truncated {
		t.Error("Truncated should be false")
	}
	if len(got.Tree) != 2 {
		t.Fatalf("Tree len = %d, want 2", len(got.Tree))
	}
	if got.Tree[0].Path != "README.md" || got.Tree[0].Type != "blob" || got.Tree[0].Size != 25 {
		t.Errorf("blob entry = %+v", got.Tree[0])
	}
	if got.Tree[1].Path != "src" || got.Tree[1].Type != "tree" {
		t.Errorf("tree entry = %+v", got.Tree[1])
	}
	// Both entry URLs should point to git/blobs/<sha>.
	if got.Tree[0].URL != "https://gitbucket.example.com/api/v3/repos/alice/demo/git/blobs/blob-sha-1" {
		t.Errorf("entry[0].URL = %q", got.Tree[0].URL)
	}
}
