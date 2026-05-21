package v3fmt

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestRepoFormatter_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	createdAt := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 21, 14, 0, 0, 0, time.UTC)

	owner := StaticUser("alice", "user:alice", "User", "")
	got := Repository(RepoSource{
		Owner:         owner,
		Name:          "demo-repo",
		Description:   "A demo repository for testing.",
		Visibility:    "public",
		DefaultBranch: "main",
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, urls)

	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	want, err := os.ReadFile("testdata/repo.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	gotStr := string(gotJSON)
	wantStr := string(want)
	if gotStr != wantStr && gotStr+"\n" != wantStr {
		t.Errorf("repo formatter drift.\nGot:\n%s\nWant:\n%s", gotStr, wantStr)
	}
}

func TestRepositoryFromMap(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	m := map[string]interface{}{
		"owner":         "alice",
		"ownerUid":      "alice-uid",
		"name":          "demo-repo",
		"description":   "x",
		"visibility":    "private",
		"defaultBranch": "main",
		"createdAt":     time.Now().UTC(),
		"updatedAt":     time.Now().UTC(),
	}
	dto := RepositoryFromMap(m, urls)
	if dto.Name != "demo-repo" {
		t.Errorf("Name = %q", dto.Name)
	}
	if !dto.Private {
		t.Error("Private should be true for visibility=private")
	}
	if dto.FullName != "alice/demo-repo" {
		t.Errorf("FullName = %q", dto.FullName)
	}
}
