package db

import (
	"context"
	"testing"
)

func TestUpdatePullRequestTitleBody(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()

	owner := "test-owner"
	repo := "test-repo"
	ownerUID := "owner-uid"

	// Seed repository
	if err := CreateRepositoryMetadata(ctx, client, ownerUID, owner, repo, "Test repo", "public"); err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	// Create a PR to update
	pr, err := CreatePullRequest(ctx, client, owner, repo, "Original Title", "Original Body", "feature", "main", ownerUID, owner)
	if err != nil {
		t.Fatalf("failed to create PR: %v", err)
	}

	t.Run("Update Both Title and Body", func(t *testing.T) {
		err := UpdatePullRequestTitleBody(ctx, client, owner, repo, pr.Number, "New Title", "New Body")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		updatedPR, err := GetPullRequest(ctx, client, owner, repo, pr.Number)
		if err != nil {
			t.Fatalf("failed to get updated PR: %v", err)
		}
		if updatedPR.Title != "New Title" {
			t.Errorf("expected title 'New Title', got '%s'", updatedPR.Title)
		}
		if updatedPR.Description != "New Body" {
			t.Errorf("expected description 'New Body', got '%s'", updatedPR.Description)
		}
	})

	t.Run("PR Does Not Exist", func(t *testing.T) {
		err := UpdatePullRequestTitleBody(ctx, client, owner, repo, 9999, "Title", "Body")
		if err == nil {
			t.Fatal("expected error when updating non-existent PR, got nil")
		}
	})
}
