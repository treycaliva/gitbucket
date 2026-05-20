package db

import (
	"context"
	"os"
	"testing"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// newTestFirestore opens a client to the local emulator (FIRESTORE_EMULATOR_HOST).
// If the emulator isn't running, the test is skipped.
func newTestFirestore(t *testing.T, ctx context.Context) *firestore.Client {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set; skipping emulator-backed test")
	}
	client, err := firestore.NewClient(ctx, "test-project")
	if err != nil {
		t.Fatalf("firestore client: %v", err)
	}
	// Clear collections between tests
	_ = clearCollection(ctx, client, "repositories")
	_ = clearCollection(ctx, client, "users")
	_ = clearCollection(ctx, client, "usernames")
	return client
}

// clearCollection deletes all documents in the given collection (best-effort, used by tests).
func clearCollection(ctx context.Context, client *firestore.Client, name string) error {
	iter := client.Collection(name).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := doc.Ref.Delete(ctx); err != nil {
			return err
		}
	}
}

func TestAddCollaborator(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()

	// Seed a repo
	if err := CreateRepositoryMetadata(ctx, client, "owner-uid", "owner", "repo1", "", "private"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Seed a user to add
	_, err := client.Collection("users").Doc("alice-uid").Set(ctx, map[string]interface{}{"username": "alice"})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	err = AddCollaborator(ctx, client, "owner", "repo1", "alice-uid", "alice", "owner-uid")
	if err != nil {
		t.Fatalf("AddCollaborator: %v", err)
	}

	collab, err := ListCollaborators(ctx, client, "owner", "repo1")
	if err != nil {
		t.Fatalf("ListCollaborators: %v", err)
	}
	if len(collab) != 1 || collab[0].Username != "alice" {
		t.Fatalf("unexpected: %+v", collab)
	}
}

func TestAddCollaboratorIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()
	_ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r", "", "private")

	if err := AddCollaborator(ctx, client, "owner", "r", "u1", "alice", "o"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := AddCollaborator(ctx, client, "owner", "r", "u1", "alice", "o"); err != nil {
		t.Fatalf("second add: %v", err)
	}

	list, _ := ListCollaborators(ctx, client, "owner", "r")
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestRemoveCollaborator(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()
	_ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r", "", "private")
	_ = AddCollaborator(ctx, client, "owner", "r", "u1", "alice", "o")

	if err := RemoveCollaborator(ctx, client, "owner", "r", "alice"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ := ListCollaborators(ctx, client, "owner", "r")
	if len(list) != 0 {
		t.Fatalf("expected 0, got %d", len(list))
	}
}
