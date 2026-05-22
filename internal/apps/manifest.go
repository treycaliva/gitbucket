package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
)

// CollectionManifestConversions is the Firestore collection name for
// one-time codes that Claude/Jules exchange for App plaintext secrets after
// completing the manifest registration flow.
const CollectionManifestConversions = "manifest_conversions"

// ManifestConversionTTL is how long a code remains valid after creation.
// Matches spec §8.1.
const ManifestConversionTTL = 10 * time.Minute

// ManifestConversion is the persisted record. The doc ID equals the code
// itself (single-use, opaque to the client). expires_at is enforced by
// both the Firestore TTL policy and an explicit check in Consume.
type ManifestConversion struct {
	Code      string    `firestore:"code"`
	AppID     string    `firestore:"app_id"`
	CreatedAt time.Time `firestore:"created_at"`
	ExpiresAt time.Time `firestore:"expires_at"`
}

// CreateManifestConversion writes a fresh code → app_id mapping with a
// 10-minute expiry. The code is returned to the SPA, which redirects the
// browser back to Claude/Jules with `?code=<code>`.
func CreateManifestConversion(ctx context.Context, fs *firestore.Client, appID string) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	if appID == "" {
		return "", fmt.Errorf("appID is required")
	}
	codeBytes := make([]byte, 24)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	code := hex.EncodeToString(codeBytes)
	now := time.Now().UTC()
	rec := ManifestConversion{
		Code:      code,
		AppID:     appID,
		CreatedAt: now,
		ExpiresAt: now.Add(ManifestConversionTTL),
	}
	if _, err := fs.Collection(CollectionManifestConversions).Doc(code).Create(ctx, rec); err != nil {
		return "", fmt.Errorf("write manifest conversion: %w", err)
	}
	return code, nil
}

// ConsumeManifestConversion atomically reads the doc, deletes it, and
// returns the associated app_id. Single-use: a second call with the same
// code returns an error. Also rejects expired codes (TTL is cleanup, not
// real-time enforcement).
func ConsumeManifestConversion(ctx context.Context, fs *firestore.Client, code string) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	docRef := fs.Collection(CollectionManifestConversions).Doc(code)

	var appID string
	err := fs.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			if isFirestoreNotFound(err) {
				return fmt.Errorf("conversion not found")
			}
			return err
		}
		var mc ManifestConversion
		if err := doc.DataTo(&mc); err != nil {
			return err
		}
		if time.Now().After(mc.ExpiresAt) {
			return fmt.Errorf("conversion expired")
		}
		appID = mc.AppID
		return tx.Delete(docRef)
	})
	return appID, err
}
