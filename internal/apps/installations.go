package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type CreateInstallationRequest struct {
	AppID               string
	Account             AccountRef
	RepositorySelection string
	RepositoryIDs       []string
	Permissions         Permissions
	Events              []string
}

func CreateInstallation(ctx context.Context, fs *firestore.Client, req CreateInstallationRequest) (*Installation, error) {
	if fs == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	if req.AppID == "" || req.Account.ID == "" {
		return nil, fmt.Errorf("AppID and Account.ID are required")
	}
	if req.RepositorySelection != "all" && req.RepositorySelection != "selected" {
		return nil, fmt.Errorf("repository_selection must be 'all' or 'selected', got %q", req.RepositorySelection)
	}
	if req.RepositorySelection == "selected" && len(req.RepositoryIDs) == 0 {
		return nil, fmt.Errorf("repository_ids required when repository_selection is 'selected'")
	}

	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate installation_id: %w", err)
	}
	id := hex.EncodeToString(idBytes)

	inst := &Installation{
		InstallationID:      id,
		AppID:               req.AppID,
		Account:             req.Account,
		RepositorySelection: req.RepositorySelection,
		RepositoryIDs:       req.RepositoryIDs,
		Permissions:         req.Permissions,
		Events:              req.Events,
		CreatedAt:           time.Now().UTC(),
	}
	if _, err := fs.Collection(CollectionInstallations).Doc(id).Create(ctx, inst); err != nil {
		return nil, fmt.Errorf("write installation: %w", err)
	}
	return inst, nil
}

// GetInstallation returns (nil, nil) if not found.
func GetInstallation(ctx context.Context, fs *firestore.Client, id string) (*Installation, error) {
	doc, err := fs.Collection(CollectionInstallations).Doc(id).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var i Installation
	if err := doc.DataTo(&i); err != nil {
		return nil, err
	}
	return &i, nil
}

// GetInstallationForApp returns the installation only if it belongs to appID.
// Returns (nil, nil) when not found OR not owned by that App — callers should
// not distinguish these to avoid leaking existence.
func GetInstallationForApp(ctx context.Context, fs *firestore.Client, id, appID string) (*Installation, error) {
	inst, err := GetInstallation(ctx, fs, id)
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.AppID != appID {
		return nil, nil
	}
	return inst, nil
}

func ListInstallationsForApp(ctx context.Context, fs *firestore.Client, appID string) ([]*Installation, error) {
	var out []*Installation
	iter := fs.Collection(CollectionInstallations).Where("app_id", "==", appID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var i Installation
		if err := doc.DataTo(&i); err != nil {
			return nil, err
		}
		out = append(out, &i)
	}
	return out, nil
}
