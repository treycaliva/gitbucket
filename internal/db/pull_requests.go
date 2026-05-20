package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PullRequest represents a Pull Request metadata in Firestore.
type PullRequest struct {
	Number         int       `json:"number" firestore:"number"`
	Title          string    `json:"title" firestore:"title"`
	Description    string    `json:"description" firestore:"description"`
	SourceBranch   string    `json:"sourceBranch" firestore:"sourceBranch"`
	TargetBranch   string    `json:"targetBranch" firestore:"targetBranch"`
	Status         string    `json:"status" firestore:"status"` // open, merged, closed
	AuthorUID      string    `json:"authorUid" firestore:"authorUid"`
	AuthorUsername string    `json:"authorUsername" firestore:"authorUsername"`
	CreatedAt      time.Time `json:"createdAt" firestore:"createdAt"`
	// RequestedReviewers is the set of usernames auto-resolved from CODEOWNERS
	// on PR open. May be empty if no CODEOWNERS rules matched the diff.
	RequestedReviewers []string `json:"requestedReviewers" firestore:"requestedReviewers"`
}

// CreatePullRequest transactionally creates a Pull Request and increments the repository's PR counter.
func CreatePullRequest(ctx context.Context, client *firestore.Client, owner, repo, title, description, sourceBranch, targetBranch, authorUid, authorUsername string) (*PullRequest, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := client.Collection("repositories").Doc(repoId)

	var pr *PullRequest
	err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(repoRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return fmt.Errorf("repository %s/%s not found", owner, repo)
			}
			return err
		}

		var repoData map[string]interface{}
		if err := doc.DataTo(&repoData); err != nil {
			return err
		}

		var counter int64
		if val, ok := repoData["pullRequestCounter"]; ok {
			switch c := val.(type) {
			case int64:
				counter = c
			case int:
				counter = int64(c)
			case float64:
				counter = int64(c)
			}
		}

		newNumber := counter + 1

		err = tx.Update(repoRef, []firestore.Update{
			{Path: "pullRequestCounter", Value: newNumber},
		})
		if err != nil {
			return err
		}

		prRef := repoRef.Collection("pulls").Doc(strconv.FormatInt(newNumber, 10))
		createdAt := time.Now()
		pr = &PullRequest{
			Number:         int(newNumber),
			Title:          title,
			Description:    description,
			SourceBranch:   sourceBranch,
			TargetBranch:   targetBranch,
			Status:         "open",
			AuthorUID:      authorUid,
			AuthorUsername: authorUsername,
			CreatedAt:      createdAt,
		}

		return tx.Set(prRef, pr)
	})

	if err != nil {
		return nil, err
	}
	return pr, nil
}

// GetPullRequest retrieves a Pull Request details from Firestore.
func GetPullRequest(ctx context.Context, client *firestore.Client, owner, repo string, number int) (*PullRequest, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	prRef := client.Collection("repositories").Doc(repoId).Collection("pulls").Doc(strconv.Itoa(number))

	doc, err := prRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}

	var pr PullRequest
	if err := doc.DataTo(&pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ListPullRequests lists all Pull Requests of a repository.
func ListPullRequests(ctx context.Context, client *firestore.Client, owner, repo string) ([]*PullRequest, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	iter := client.Collection("repositories").Doc(repoId).Collection("pulls").OrderBy("number", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var prs []*PullRequest
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var pr PullRequest
		if err := doc.DataTo(&pr); err != nil {
			return nil, err
		}
		prs = append(prs, &pr)
	}

	if prs == nil {
		prs = []*PullRequest{}
	}
	return prs, nil
}

// UpdatePullRequestReviewers replaces the requestedReviewers field on a Pull Request.
func UpdatePullRequestReviewers(ctx context.Context, client *firestore.Client, owner, repo string, number int, reviewers []string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	if reviewers == nil {
		reviewers = []string{}
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	prRef := client.Collection("repositories").Doc(repoId).Collection("pulls").Doc(strconv.Itoa(number))

	_, err := prRef.Update(ctx, []firestore.Update{
		{Path: "requestedReviewers", Value: reviewers},
	})
	return err
}

// UpdatePullRequestStatus updates the status (open, closed, merged) of a Pull Request.
func UpdatePullRequestStatus(ctx context.Context, client *firestore.Client, owner, repo string, number int, newStatus string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	prRef := client.Collection("repositories").Doc(repoId).Collection("pulls").Doc(strconv.Itoa(number))

	_, err := prRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: newStatus},
	})
	return err
}
