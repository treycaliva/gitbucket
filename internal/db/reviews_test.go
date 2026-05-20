package db

import (
	"context"
	"testing"
	"time"
)

func TestReviewUpsertAndList(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()

	if err := CreateRepositoryMetadata(ctx, client, "owner-uid", "owner", "repo1", "", "private"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	pr, err := CreatePullRequest(ctx, client, "owner", "repo1", "T", "D", "feat", "main", "owner-uid", "owner")
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}

	rev := Review{UID: "alice-uid", Username: "alice", State: "approved", Body: "lgtm"}
	if err := UpsertReview(ctx, client, "owner", "repo1", pr.Number, rev); err != nil {
		t.Fatalf("UpsertReview: %v", err)
	}

	list, err := ListReviews(ctx, client, "owner", "repo1", pr.Number)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 review, got %d", len(list))
	}
	if list[0].Username != "alice" || list[0].State != "approved" || list[0].Body != "lgtm" {
		t.Fatalf("unexpected review: %+v", list[0])
	}
	if list[0].SubmittedAt.IsZero() {
		t.Fatal("expected SubmittedAt to be set")
	}
}

func TestReviewUpsertReplacesPrior(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()

	_ = CreateRepositoryMetadata(ctx, client, "owner-uid", "owner", "r", "", "private")
	pr, err := CreatePullRequest(ctx, client, "owner", "r", "T", "D", "feat", "main", "owner-uid", "owner")
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}

	first := Review{UID: "u1", Username: "alice", State: "commented", Body: "wip"}
	if err := UpsertReview(ctx, client, "owner", "r", pr.Number, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Force a clear timestamp delta so ordering would be deterministic if relevant.
	time.Sleep(5 * time.Millisecond)
	second := Review{UID: "u1", Username: "alice", State: "approved", Body: "lgtm"}
	if err := UpsertReview(ctx, client, "owner", "r", pr.Number, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	list, err := ListReviews(ctx, client, "owner", "r", pr.Number)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 review (replaced), got %d", len(list))
	}
	if list[0].State != "approved" {
		t.Fatalf("expected state=approved after replace, got %q", list[0].State)
	}
}

func TestReviewListEmpty(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()

	// Use a unique repo name so orphaned subcollections from prior tests
	// (Firestore emulator doesn't recursively delete subcollections when
	// the parent doc is removed) don't bleed in.
	_ = CreateRepositoryMetadata(ctx, client, "o", "owner", "reviews-empty", "", "private")
	pr, _ := CreatePullRequest(ctx, client, "owner", "reviews-empty", "T", "D", "feat", "main", "o", "owner")

	list, err := ListReviews(ctx, client, "owner", "reviews-empty", pr.Number)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0, got %d", len(list))
	}
}

func TestUpdatePullRequestReviewers(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()

	_ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r", "", "private")
	pr, _ := CreatePullRequest(ctx, client, "owner", "r", "T", "D", "feat", "main", "o", "owner")

	if err := UpdatePullRequestReviewers(ctx, client, "owner", "r", pr.Number, []string{"alice", "bob"}); err != nil {
		t.Fatalf("UpdatePullRequestReviewers: %v", err)
	}

	got, err := GetPullRequest(ctx, client, "owner", "r", pr.Number)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if len(got.RequestedReviewers) != 2 || got.RequestedReviewers[0] != "alice" || got.RequestedReviewers[1] != "bob" {
		t.Fatalf("unexpected reviewers: %+v", got.RequestedReviewers)
	}
}
