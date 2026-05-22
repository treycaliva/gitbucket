package v3fmt

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestPullFormatter_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	created := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	author := StaticUser("alice", "user:alice", "User", "")

	got := PullRequest(PullRequestSource{
		Owner:      "alice",
		Repo:       "demo",
		Number:     1,
		Title:      "Add feature X",
		Body:       "This adds feature X.",
		State:      "open",
		Author:     author,
		HeadBranch: "feature",
		BaseBranch: "main",
		CreatedAt:  created,
		UpdatedAt:  created,
	}, urls)

	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	want, err := os.ReadFile("testdata/pull.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	gotStr := string(gotJSON)
	wantStr := string(want)
	if gotStr != wantStr && gotStr+"\n" != wantStr {
		t.Errorf("pull formatter drift.\nGot:\n%s\nWant:\n%s", gotStr, wantStr)
	}
}

func TestPullFormatter_MergedMapsToClosedPlusMerged(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	mergedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	author := StaticUser("bob", "user:bob", "User", "")
	got := PullRequest(PullRequestSource{
		Owner: "alice", Repo: "demo", Number: 2,
		Title: "X", State: "merged", Author: author,
		HeadBranch: "f", BaseBranch: "main",
		CreatedAt: mergedAt, UpdatedAt: mergedAt,
		MergedAt: &mergedAt,
	}, urls)
	if got.State != "closed" {
		t.Errorf("State = %q, want closed (GitHub maps merged → closed+merged:true)", got.State)
	}
	if !got.Merged {
		t.Error("Merged should be true")
	}
}

func TestPullFormatter_NumberInURLs(t *testing.T) {
	urls := NewURLBuilder("https://x.test")
	author := StaticUser("a", "user:a", "User", "")
	got := PullRequest(PullRequestSource{
		Owner: "o", Repo: "r", Number: 42, Title: "t", State: "open", Author: author,
		HeadBranch: "h", BaseBranch: "b",
	}, urls)
	if got.URL != "https://x.test/api/v3/repos/o/r/pulls/42" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.HTMLURL != "https://x.test/o/r/pulls/42" {
		t.Errorf("HTMLURL = %q", got.HTMLURL)
	}
}
