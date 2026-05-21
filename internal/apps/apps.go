// internal/apps/apps.go
package apps

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateAppRequest is the input to CreateApp. All fields are required except
// DefaultEvents (which falls back to a built-in baseline).
type CreateAppRequest struct {
	Slug               string
	Name               string
	OwnerAccount       AccountRef
	BotUserID          string
	WebhookURL         string
	DefaultPermissions Permissions
	DefaultEvents      []string
}

// AppSecrets carries the plaintext secrets returned exactly once from CreateApp.
// Callers MUST relay these to the registering App and then drop the references.
type AppSecrets struct {
	ClientID      string
	ClientSecret  string
	WebhookSecret string
	PrivateKeyPEM string
}

var defaultEventsBaseline = []string{
	"issues", "issue_comment", "pull_request", "pull_request_review_comment", "push", "installation",
}

// CreateApp generates RSA-2048 keypair, persists private key + webhook secret to
// SecretStore, writes the apps/{app_id} document, and returns the App + the
// one-time plaintext secrets bundle.
func CreateApp(ctx context.Context, fs *firestore.Client, store SecretStore, req CreateAppRequest) (*App, *AppSecrets, error) {
	if fs == nil {
		return nil, nil, fmt.Errorf("firestore client is nil")
	}
	if store == nil {
		return nil, nil, fmt.Errorf("secret store is nil")
	}
	if req.Slug == "" || req.Name == "" || req.OwnerAccount.ID == "" || req.BotUserID == "" || req.WebhookURL == "" {
		return nil, nil, fmt.Errorf("missing required CreateAppRequest field")
	}

	// Uniqueness pre-check: reject if slug is already taken. The final write
	// also runs in a transaction that re-checks, but pre-checking avoids
	// generating an RSA key for nothing.
	existing, err := GetAppBySlug(ctx, fs, req.Slug)
	if err != nil {
		return nil, nil, fmt.Errorf("slug uniqueness check: %w", err)
	}
	if existing != nil {
		return nil, nil, fmt.Errorf("app slug %q already taken", req.Slug)
	}

	appIDBytes := make([]byte, 16)
	if _, err := rand.Read(appIDBytes); err != nil {
		return nil, nil, fmt.Errorf("generate app_id: %w", err)
	}
	appID := hex.EncodeToString(appIDBytes)

	clientIDBytes := make([]byte, 12)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return nil, nil, fmt.Errorf("generate client_id: %w", err)
	}
	clientID := "Iv1." + hex.EncodeToString(clientIDBytes)

	clientSecret, err := randomToken(32)
	if err != nil {
		return nil, nil, fmt.Errorf("generate client_secret: %w", err)
	}
	webhookSecret, err := randomToken(32)
	if err != nil {
		return nil, nil, fmt.Errorf("generate webhook_secret: %w", err)
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate rsa key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal pubkey: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	privResource, err := store.Put(ctx, fmt.Sprintf("apps/%s/private-key", appID), privPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("store private key: %w", err)
	}
	webhookResource, err := store.Put(ctx, fmt.Sprintf("apps/%s/webhook-secret", appID), []byte(webhookSecret))
	if err != nil {
		// Best-effort rollback.
		_ = store.Delete(ctx, privResource)
		return nil, nil, fmt.Errorf("store webhook secret: %w", err)
	}

	defaultEvents := req.DefaultEvents
	if len(defaultEvents) == 0 {
		defaultEvents = defaultEventsBaseline
	}

	now := time.Now().UTC()
	app := &App{
		AppID:                 appID,
		Slug:                  req.Slug,
		Name:                  req.Name,
		OwnerAccount:          req.OwnerAccount,
		BotUserID:             req.BotUserID,
		ClientID:              clientID,
		ClientSecretHash:      sha256Hex(clientSecret),
		WebhookURL:            req.WebhookURL,
		WebhookSecretResource: webhookResource,
		WebhookSecretHash:     sha256Hex(webhookSecret),
		PrivateKeySecret:      privResource,
		PublicKeyPEM:          string(pubPEM),
		DefaultPermissions:    req.DefaultPermissions,
		DefaultEvents:         defaultEvents,
		SuspendedAt:           nil,
		CreatedAt:             now,
	}

	appRef := fs.Collection(CollectionApps).Doc(appID)
	if _, err := appRef.Create(ctx, app); err != nil {
		_ = store.Delete(ctx, privResource)
		_ = store.Delete(ctx, webhookResource)
		return nil, nil, fmt.Errorf("write app doc: %w", err)
	}

	return app, &AppSecrets{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		WebhookSecret: webhookSecret,
		PrivateKeyPEM: string(privPEM),
	}, nil
}

// GetApp loads an App by its app_id. Returns (nil, nil) if not found.
func GetApp(ctx context.Context, fs *firestore.Client, appID string) (*App, error) {
	doc, err := fs.Collection(CollectionApps).Doc(appID).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var a App
	if err := doc.DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAppBySlug returns (nil, nil) if no App with this slug exists.
func GetAppBySlug(ctx context.Context, fs *firestore.Client, slug string) (*App, error) {
	iter := fs.Collection(CollectionApps).Where("slug", "==", slug).Limit(1).Documents(ctx)
	defer iter.Stop()
	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a App
	if err := doc.DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// --- internal helpers ------------------------------------------------------

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isFirestoreNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
