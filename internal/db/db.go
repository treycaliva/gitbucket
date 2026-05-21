package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DBClient handles Firestore queries.
type DBClient struct {
	Client *firestore.Client
}

// NewClient initializes the Firestore client.
func NewClient(ctx context.Context, projectID string) (*firestore.Client, error) {
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create firestore client: %w", err)
	}
	return client, nil
}

// GetUsernameByUID fetches the username for the given UID from the "users" collection.
func GetUsernameByUID(ctx context.Context, client *firestore.Client, uid string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	doc, err := client.Collection("users").Doc(uid).Get(ctx)
	if err != nil {
		return "", err
	}
	usernameVal, err := doc.DataAt("username")
	if err != nil {
		return "", err
	}
	username, ok := usernameVal.(string)
	if !ok {
		return "", fmt.Errorf("username is not a string")
	}
	return username, nil
}

// GetUidByUsername fetches the UID for the given username.
func GetUidByUsername(ctx context.Context, client *firestore.Client, username string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	doc, err := client.Collection("usernames").Doc(username).Get(ctx)
	if err != nil {
		return "", err
	}
	uidVal, err := doc.DataAt("uid")
	if err != nil {
		return "", err
	}
	uid, ok := uidVal.(string)
	if !ok {
		return "", fmt.Errorf("uid is not a string")
	}
	return uid, nil
}

// RegisterUsername transactionally registers a unique username mapping and user profile.
func RegisterUsername(ctx context.Context, client *firestore.Client, uid, username, email string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	usernameDocRef := client.Collection("usernames").Doc(username)
	userDocRef := client.Collection("users").Doc(uid)

	err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Check if the username document already exists.
		usernameDoc, err := tx.Get(usernameDocRef)
		if err == nil && usernameDoc.Exists() {
			return fmt.Errorf("username already taken")
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}

		// Check if the user document already exists and has a username.
		userDoc, err := tx.Get(userDocRef)
		if err == nil && userDoc.Exists() {
			var userData map[string]interface{}
			if err := userDoc.DataTo(&userData); err == nil {
				if existingUsername, ok := userData["username"].(string); ok && existingUsername != "" {
					return fmt.Errorf("user already has a username registered")
				}
			}
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}

		// Write to both usernames and users collections.
		if err := tx.Set(usernameDocRef, map[string]interface{}{"uid": uid}); err != nil {
			return err
		}
		return tx.Set(userDocRef, map[string]interface{}{
			"uid":      uid,
			"username": username,
			"email":    email,
		}, firestore.MergeAll)
	})
	return err
}

// PATMetadata represents public personal access token details.
type PATMetadata struct {
	ID         string     `json:"id" firestore:"id"`
	Name       string     `json:"name" firestore:"name"`
	CreatedAt  time.Time  `json:"createdAt" firestore:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt" firestore:"lastUsedAt"`
}

// Collaborator represents a non-owner user with access to a repository.
type Collaborator struct {
	UID      string    `json:"uid" firestore:"uid"`
	Username string    `json:"username" firestore:"username"`
	AddedAt  time.Time `json:"addedAt" firestore:"addedAt"`
	AddedBy  string    `json:"addedBy" firestore:"addedBy"`
}

// GitHubSyncConfig represents the synchronization settings for GitHub.
type GitHubSyncConfig struct {
	Enabled      bool      `json:"enabled" firestore:"enabled"`
	GitHubRepo   string    `json:"githubRepo" firestore:"githubRepo"` // e.g. "owner/repo"
	Direction    string    `json:"direction" firestore:"direction"`   // "mirror-to-github" | "mirror-to-gitbucket" | "bidirectional"
	SyncStatus   string    `json:"syncStatus" firestore:"syncStatus"` // "synced" | "syncing" | "conflict" | "error"
	LastSyncedAt time.Time `json:"lastSyncedAt" firestore:"lastSyncedAt"`
	ErrorMessage string    `json:"errorMessage" firestore:"errorMessage"`
}

// RepositoryMetadata holds repository details.
type RepositoryMetadata struct {
	OwnerUID               string           `json:"ownerUid" firestore:"ownerUid"`
	Owner                  string           `json:"owner" firestore:"owner"`
	Name                   string           `json:"name" firestore:"name"`
	Description            string           `json:"description" firestore:"description"`
	Visibility             string           `json:"visibility" firestore:"visibility"`
	DefaultBranch          string           `json:"defaultBranch" firestore:"defaultBranch"`
	CreatedAt              time.Time        `json:"createdAt" firestore:"createdAt"`
	UpdatedAt              time.Time        `json:"updatedAt" firestore:"updatedAt"`
	Branches               []string         `json:"branches" firestore:"branches"`
	AutoDeleteHeadBranches bool             `json:"autoDeleteHeadBranches" firestore:"autoDeleteHeadBranches"`
	Collaborators          []Collaborator   `json:"collaborators" firestore:"collaborators"`
	GitHubSync             GitHubSyncConfig `json:"githubSync" firestore:"githubSync"`
}

// MapToRepositoryMetadata converts a Firestore document data map to RepositoryMetadata.
func MapToRepositoryMetadata(m map[string]interface{}) *RepositoryMetadata {
	if m == nil {
		return nil
	}
	meta := &RepositoryMetadata{}
	if val, ok := m["ownerUid"].(string); ok {
		meta.OwnerUID = val
	}
	if val, ok := m["owner"].(string); ok {
		meta.Owner = val
	}
	if val, ok := m["name"].(string); ok {
		meta.Name = val
	}
	if val, ok := m["description"].(string); ok {
		meta.Description = val
	}
	if val, ok := m["visibility"].(string); ok {
		meta.Visibility = val
	}
	if val, ok := m["defaultBranch"].(string); ok {
		meta.DefaultBranch = val
	}
	if val, ok := m["createdAt"].(time.Time); ok {
		meta.CreatedAt = val
	}
	if val, ok := m["updatedAt"].(time.Time); ok {
		meta.UpdatedAt = val
	}
	if val, ok := m["autoDeleteHeadBranches"].(bool); ok {
		meta.AutoDeleteHeadBranches = val
	}
	if val, ok := m["branches"].([]interface{}); ok {
		for _, b := range val {
			if s, ok := b.(string); ok {
				meta.Branches = append(meta.Branches, s)
			}
		}
	} else if val, ok := m["branches"].([]string); ok {
		meta.Branches = val
	}
	if val, ok := m["collaborators"].([]interface{}); ok {
		for _, c := range val {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			collab := Collaborator{}
			if s, ok := cm["uid"].(string); ok {
				collab.UID = s
			}
			if s, ok := cm["username"].(string); ok {
				collab.Username = s
			}
			if t, ok := cm["addedAt"].(time.Time); ok {
				collab.AddedAt = t
			}
			if s, ok := cm["addedBy"].(string); ok {
				collab.AddedBy = s
			}
			meta.Collaborators = append(meta.Collaborators, collab)
		}
	}
	if val, ok := m["githubSync"].(map[string]interface{}); ok {
		meta.GitHubSync.Enabled, _ = val["enabled"].(bool)
		meta.GitHubSync.GitHubRepo, _ = val["githubRepo"].(string)
		meta.GitHubSync.Direction, _ = val["direction"].(string)
		meta.GitHubSync.SyncStatus, _ = val["syncStatus"].(string)
		if t, ok := val["lastSyncedAt"].(time.Time); ok {
			meta.GitHubSync.LastSyncedAt = t
		}
		meta.GitHubSync.ErrorMessage, _ = val["errorMessage"].(string)
	}
	return meta
}

// hashToken helper function hashes a PAT raw token.
func hashToken(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

// GeneratePAT creates a raw gb_pat_... token and saves its hash in the tokens collection.
func GeneratePAT(ctx context.Context, client *firestore.Client, uid, name string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	rawToken := "gb_pat_" + hex.EncodeToString(b)
	hashed := hashToken(rawToken)

	docRef := client.Collection("tokens").NewDoc()
	_, err := docRef.Set(ctx, map[string]interface{}{
		"id":          docRef.ID,
		"name":        name,
		"hashedToken": hashed,
		"uid":         uid,
		"createdAt":   firestore.ServerTimestamp,
		"lastUsedAt":  nil,
	})
	if err != nil {
		return "", fmt.Errorf("failed to save token document: %w", err)
	}

	return rawToken, nil
}

// VerifyPAT hashes the token, queries the tokens collection, updates lastUsedAt, and returns the owner's UID and Username.
func VerifyPAT(ctx context.Context, client *firestore.Client, token string) (string, string, error) {
	if client == nil {
		return "", "", fmt.Errorf("firestore client is nil")
	}
	hashed := hashToken(token)

	iter := client.Collection("tokens").Where("hashedToken", "==", hashed).Limit(1).Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err != nil {
		return "", "", fmt.Errorf("invalid or expired token")
	}

	var tokenData map[string]interface{}
	if err := doc.DataTo(&tokenData); err != nil {
		return "", "", err
	}

	uid, ok := tokenData["uid"].(string)
	if !ok || uid == "" {
		return "", "", fmt.Errorf("token document has invalid uid")
	}

	_, err = doc.Ref.Update(ctx, []firestore.Update{
		{Path: "lastUsedAt", Value: firestore.ServerTimestamp},
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to update lastUsedAt: %w", err)
	}

	username, err := GetUsernameByUID(ctx, client, uid)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch username for token owner: %w", err)
	}

	return uid, username, nil
}

// ListPATs retrieves all PATs owned by the user.
func ListPATs(ctx context.Context, client *firestore.Client, uid string) ([]PATMetadata, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	var list []PATMetadata
	iter := client.Collection("tokens").Where("uid", "==", uid).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var p PATMetadata
		if err := doc.DataTo(&p); err != nil {
			return nil, err
		}
		if p.ID == "" {
			p.ID = doc.Ref.ID
		}
		list = append(list, p)
	}
	return list, nil
}

// RevokePAT deletes the token if owned by the user.
func RevokePAT(ctx context.Context, client *firestore.Client, uid, tokenId string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	docRef := client.Collection("tokens").Doc(tokenId)

	err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		var tokenData map[string]interface{}
		if err := doc.DataTo(&tokenData); err != nil {
			return err
		}
		ownerUID, _ := tokenData["uid"].(string)
		if ownerUID != uid {
			return fmt.Errorf("unauthorized to revoke this token")
		}
		return tx.Delete(docRef)
	})
	return err
}

// AcquireLock attempts to acquire a distributed lock on a repository.
func AcquireLock(ctx context.Context, client *firestore.Client, owner, repo string, leaseDuration, timeout time.Duration) (string, error) {
	if client == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	start := time.Now()
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	lockId := fmt.Sprintf("%s_%s", owner, repo)
	lockRef := client.Collection("locks").Doc(lockId)

	for {
		var acquired bool
		err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			acquired = false
			doc, err := tx.Get(lockRef)
			now := time.Now()
			if err != nil {
				if status.Code(err) != codes.NotFound {
					return err
				}
				// Lock doesn't exist
			} else if doc.Exists() {
				expiresAtVal, err := doc.DataAt("expiresAt")
				if err == nil {
					expiresAt, ok := expiresAtVal.(time.Time)
					if ok && now.Before(expiresAt) {
						// Lock is still active
						return nil
					}
				}
			}

			// Lock does not exist or has expired, acquire it
			expiresAt := now.Add(leaseDuration)
			err = tx.Set(lockRef, map[string]interface{}{
				"acquiredBy": token,
				"acquiredAt": now,
				"expiresAt":  expiresAt,
			})
			if err != nil {
				return err
			}
			acquired = true
			return nil
		})

		if err == nil && acquired {
			return token, nil
		}

		if time.Since(start) >= timeout {
			break
		}

		// Wait and retry
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return "", fmt.Errorf("timeout acquiring lock for repository %s/%s", owner, repo)
}

// ReleaseLock releases the lock if the token matches.
func ReleaseLock(ctx context.Context, client *firestore.Client, owner, repo, token string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	lockId := fmt.Sprintf("%s_%s", owner, repo)
	lockRef := client.Collection("locks").Doc(lockId)

	err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(lockRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				// Lock doesn't exist, nothing to do
				return nil
			}
			return err
		}

		acquiredByVal, err := doc.DataAt("acquiredBy")
		if err != nil {
			return err
		}
		acquiredBy, ok := acquiredByVal.(string)
		if ok && acquiredBy == token {
			return tx.Delete(lockRef)
		}
		return nil
	})
	return err
}

// CreateRepositoryMetadata transactionally creates a unique repository document.
func CreateRepositoryMetadata(ctx context.Context, client *firestore.Client, ownerUid, ownerUsername, repoName, description, visibility string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(ownerUsername), strings.ToLower(repoName))
	repoRef := client.Collection("repositories").Doc(repoId)

	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(repoRef)
		if err == nil && doc.Exists() {
			return fmt.Errorf("repository %s/%s already exists", ownerUsername, repoName)
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}

		if visibility == "" {
			visibility = "public"
		}

		return tx.Set(repoRef, map[string]interface{}{
			"ownerUid":      ownerUid,
			"owner":         strings.ToLower(ownerUsername),
			"name":          repoName,
			"description":   description,
			"visibility":    visibility,
			"defaultBranch": "main",
			"createdAt":     firestore.ServerTimestamp,
			"updatedAt":     firestore.ServerTimestamp,
			"branches":      []string{"main"},
			"commitsCache":  []interface{}{},
		})
	})
}

// GetRepositoryMetadata retrieves repository metadata from Firestore.
func GetRepositoryMetadata(ctx context.Context, client *firestore.Client, owner, repo string) (map[string]interface{}, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	doc, err := client.Collection("repositories").Doc(repoId).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	return doc.Data(), nil
}

// UpdateRepositoryMetadata updates specific repository metadata fields.
func UpdateRepositoryMetadata(ctx context.Context, client *firestore.Client, owner, repo string, data map[string]interface{}) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	repoRef := client.Collection("repositories").Doc(repoId)

	updates := make([]firestore.Update, 0, len(data)+1)
	for k, v := range data {
		updates = append(updates, firestore.Update{Path: k, Value: v})
	}
	updates = append(updates, firestore.Update{Path: "updatedAt", Value: firestore.ServerTimestamp})

	_, err := repoRef.Update(ctx, updates)
	return err
}

// DeleteRepositoryMetadata deletes repository metadata from Firestore.
func DeleteRepositoryMetadata(ctx context.Context, client *firestore.Client, owner, repo string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
	_, err := client.Collection("repositories").Doc(repoId).Delete(ctx)
	return err
}

// ListUserRepositories lists all repositories owned by a user.
func ListUserRepositories(ctx context.Context, client *firestore.Client, owner string) ([]map[string]interface{}, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	var repos []map[string]interface{}
	iter := client.Collection("repositories").Where("owner", "==", strings.ToLower(owner)).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		repos = append(repos, doc.Data())
	}
	return repos, nil
}

// ListAllPublicRepositories lists all public repositories.
func ListAllPublicRepositories(ctx context.Context, client *firestore.Client) ([]map[string]interface{}, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	var repos []map[string]interface{}
	iter := client.Collection("repositories").Where("visibility", "==", "public").Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		repos = append(repos, doc.Data())
	}
	return repos, nil
}
