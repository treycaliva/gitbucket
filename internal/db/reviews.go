package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// Review represents a single PR review submitted by a user. Stored under
// repositories/{repoId}/pulls/{n}/reviews/{uid}. One review per (PR, user);
// re-submitting replaces the previous review.
type Review struct {
	UID         string    `json:"uid" firestore:"uid"`
	Username    string    `json:"username" firestore:"username"`
	State       string    `json:"state" firestore:"state"` // approved | changes_requested | commented
	Body        string    `json:"body" firestore:"body"`
	SubmittedAt time.Time `json:"submittedAt" firestore:"submittedAt"`
}

func reviewsCol(client *firestore.Client, owner, repo string, n int) *firestore.CollectionRef {
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	return client.Collection("repositories").Doc(repoId).Collection("pulls").Doc(strconv.Itoa(n)).Collection("reviews")
}

// UpsertReview stores a review keyed by reviewer UID. Re-submission replaces
// the prior review. SubmittedAt is set to time.Now() server-side.
func UpsertReview(ctx context.Context, client *firestore.Client, owner, repo string, n int, r Review) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	if r.UID == "" {
		return fmt.Errorf("review UID is required")
	}
	r.SubmittedAt = time.Now()
	_, err := reviewsCol(client, owner, repo, n).Doc(r.UID).Set(ctx, r)
	return err
}

// ListReviews returns all reviews for a PR, ordered by submittedAt ascending.
func ListReviews(ctx context.Context, client *firestore.Client, owner, repo string, n int) ([]Review, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	iter := reviewsCol(client, owner, repo, n).OrderBy("submittedAt", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var out []Review
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var rv Review
		if err := doc.DataTo(&rv); err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	if out == nil {
		out = []Review{}
	}
	return out, nil
}
