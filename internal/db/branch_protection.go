package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"gitbucket/internal/git"
)

// ruleID returns a deterministic short identifier for a rule pattern.
func ruleID(pattern string) string {
	h := sha256.Sum256([]byte(pattern))
	return hex.EncodeToString(h[:8])
}

// rulesCol returns the branch-protection subcollection for the given repo.
func rulesCol(client *firestore.Client, owner, repo string) *firestore.CollectionRef {
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	return client.Collection("repositories").Doc(repoId).Collection("branchProtection")
}

// CreateRule writes the rule with a deterministic ID derived from its pattern.
func CreateRule(ctx context.Context, client *firestore.Client, owner, repo string, rule git.Rule) (git.Rule, error) {
	if client == nil {
		return git.Rule{}, fmt.Errorf("firestore client is nil")
	}
	rule.ID = ruleID(rule.Pattern)
	if _, err := rulesCol(client, owner, repo).Doc(rule.ID).Set(ctx, rule); err != nil {
		return git.Rule{}, err
	}
	return rule, nil
}

// ListRules returns every rule attached to the repo (unordered).
func ListRules(ctx context.Context, client *firestore.Client, owner, repo string) ([]git.Rule, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	iter := rulesCol(client, owner, repo).Documents(ctx)
	defer iter.Stop()
	var out []git.Rule
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var r git.Rule
		if err := doc.DataTo(&r); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// GetRule fetches one rule by ID. Returns nil, nil if the rule does not exist.
func GetRule(ctx context.Context, client *firestore.Client, owner, repo, id string) (*git.Rule, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	doc, err := rulesCol(client, owner, repo).Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var r git.Rule
	if err := doc.DataTo(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateRule replaces the rule document. The ID is always rederived from the pattern,
// so renaming a pattern produces a new doc; callers wanting a rename should delete + create.
func UpdateRule(ctx context.Context, client *firestore.Client, owner, repo string, rule git.Rule) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	rule.ID = ruleID(rule.Pattern)
	_, err := rulesCol(client, owner, repo).Doc(rule.ID).Set(ctx, rule)
	return err
}

// DeleteRule removes a rule. No-op if it does not exist.
func DeleteRule(ctx context.Context, client *firestore.Client, owner, repo, id string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	_, err := rulesCol(client, owner, repo).Doc(id).Delete(ctx)
	return err
}
