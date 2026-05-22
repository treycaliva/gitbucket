package v3fmt

import (
	"encoding/json"
	"os"
	"testing"
)

func TestContentsFile_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := ContentFile(ContentFileSource{
		Owner: "alice", Repo: "demo", Path: "README.md", Ref: "main",
		SHA:  "abc123def456",
		Size: 42,
		Raw:  []byte("# Hello\n\nThis is a test.\n"),
	}, urls)
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	want, _ := os.ReadFile("testdata/contents-file.golden.json")
	if string(gotJSON) != string(want) && string(gotJSON)+"\n" != string(want) {
		t.Errorf("contents file drift.\nGot:\n%s\nWant:\n%s", gotJSON, want)
	}
}

func TestContentsDir_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := ContentDir(ContentDirSource{
		Owner: "alice", Repo: "demo", Path: "src", Ref: "main",
		Entries: []DirEntry{
			{Name: "main.go", Path: "src/main.go", SHA: "sha-of-main", Type: "file", Size: 120},
			{Name: "helpers", Path: "src/helpers", SHA: "sha-of-helpers", Type: "dir"},
		},
	}, urls)
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	want, _ := os.ReadFile("testdata/contents-dir.golden.json")
	if string(gotJSON) != string(want) && string(gotJSON)+"\n" != string(want) {
		t.Errorf("contents dir drift.\nGot:\n%s\nWant:\n%s", gotJSON, want)
	}
}
