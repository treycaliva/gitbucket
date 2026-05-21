package apps

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

const (
	tokenPrefix   = "ghs_"
	tokenEntropyN = 24 // 24 bytes → 39 base32 chars; total token len ~43
	TokenTTL      = time.Hour
)

// MintRequest carries optional narrowing of the issued token's scope.
type MintRequest struct {
	Permissions   Permissions
	RepositoryIDs []string
}

// MintOutput holds the one-time plaintext and the persisted record.
type MintOutput struct {
	Plaintext string             // returned exactly once
	Record    *InstallationToken // the persisted record (without the plaintext)
}

// MintInstallationToken validates that the requested permissions / repo subset
// are within the installation's grant, generates a ghs_-prefixed token,
// persists the sha256(token) as the document ID, and returns the plaintext.
func MintInstallationToken(ctx context.Context, fs *firestore.Client, inst *Installation, req MintRequest) (*MintOutput, error) {
	if inst == nil {
		return nil, fmt.Errorf("nil installation")
	}
	perms := req.Permissions
	if len(perms) == 0 {
		perms = inst.Permissions
	} else {
		for scope, need := range perms {
			if !inst.Permissions.Satisfies(scope, need) {
				return nil, fmt.Errorf("requested permission %s:%s not granted to installation", scope, need.String())
			}
		}
	}

	repoIDs := req.RepositoryIDs
	if len(repoIDs) == 0 {
		repoIDs = inst.RepositoryIDs
	} else if inst.RepositorySelection == "selected" {
		allowed := map[string]bool{}
		for _, id := range inst.RepositoryIDs {
			allowed[id] = true
		}
		for _, id := range repoIDs {
			if !allowed[id] {
				return nil, fmt.Errorf("repository %s not granted to installation", id)
			}
		}
	}

	plaintext, err := generateGHSToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	now := time.Now().UTC()
	rec := &InstallationToken{
		InstallationID: inst.InstallationID,
		Permissions:    perms,
		RepositoryIDs:  repoIDs,
		IssuedAt:       now,
		ExpiresAt:      now.Add(TokenTTL),
	}
	hash := sha256Hex(plaintext)
	if _, err := fs.Collection(CollectionInstallationTokens).Doc(hash).Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("persist token: %w", err)
	}
	return &MintOutput{Plaintext: plaintext, Record: rec}, nil
}

// VerifyInstallationToken hashes the inbound token, point-reads the record,
// and rejects missing or expired tokens. Expiry is checked explicitly here —
// Firestore TTL is a sweep, not real-time.
func VerifyInstallationToken(ctx context.Context, fs *firestore.Client, plaintext string) (*InstallationToken, error) {
	if !strings.HasPrefix(plaintext, tokenPrefix) {
		return nil, fmt.Errorf("invalid token format")
	}
	hash := sha256Hex(plaintext)
	doc, err := fs.Collection(CollectionInstallationTokens).Doc(hash).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, fmt.Errorf("token not found")
		}
		return nil, err
	}
	var t InstallationToken
	if err := doc.DataTo(&t); err != nil {
		return nil, err
	}
	if t.Expired(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}
	return &t, nil
}

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

func generateGHSToken() (string, error) {
	b := make([]byte, tokenEntropyN)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return tokenPrefix + strings.ToLower(base32NoPad.EncodeToString(b)), nil
}
