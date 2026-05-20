package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// AddCollaborator appends a collaborator to the repo's collaborators array.
// No-op if a collaborator with the same UID already exists.
func AddCollaborator(ctx context.Context, client *firestore.Client, owner, repo, uid, username, addedBy string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := client.Collection("repositories").Doc(repoId)

	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(repoRef)
		if err != nil {
			return err
		}
		data := doc.Data()

		existing, _ := data["collaborators"].([]interface{})
		for _, c := range existing {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if cm["uid"] == uid {
				return nil
			}
		}
		entry := map[string]interface{}{
			"uid":      uid,
			"username": username,
			"addedAt":  time.Now(),
			"addedBy":  addedBy,
		}
		return tx.Update(repoRef, []firestore.Update{
			{Path: "collaborators", Value: append(existing, entry)},
		})
	})
}

// ListCollaborators returns the collaborators on a repo.
func ListCollaborators(ctx context.Context, client *firestore.Client, owner, repo string) ([]Collaborator, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	doc, err := client.Collection("repositories").Doc(repoId).Get(ctx)
	if err != nil {
		return nil, err
	}
	meta := MapToRepositoryMetadata(doc.Data())
	if meta == nil || meta.Collaborators == nil {
		return []Collaborator{}, nil
	}
	return meta.Collaborators, nil
}

// RemoveCollaborator removes the collaborator with the given username. No-op if not present.
func RemoveCollaborator(ctx context.Context, client *firestore.Client, owner, repo, username string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := client.Collection("repositories").Doc(repoId)
	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(repoRef)
		if err != nil {
			return err
		}
		meta := MapToRepositoryMetadata(doc.Data())
		if meta == nil {
			return nil
		}
		out := make([]Collaborator, 0, len(meta.Collaborators))
		for _, c := range meta.Collaborators {
			if c.Username != username {
				out = append(out, c)
			}
		}
		return tx.Update(repoRef, []firestore.Update{{Path: "collaborators", Value: out}})
	})
}
