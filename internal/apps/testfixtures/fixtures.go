// internal/apps/testfixtures/fixtures.go
// Package testfixtures provides reusable seed helpers for tests that need an
// App + Installation + working installation token. Used by tests in plans
// 2, 3, and 4. NOT for production use.
package testfixtures

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/golang-jwt/jwt/v4"

	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

// Scenario bundles a freshly-seeded App + Installation + decoded private key,
// so a test can sign JWTs and mint installation tokens with one line.
type Scenario struct {
	FS           *firestore.Client
	Store        apps.SecretStore
	App          *apps.App
	Installation *apps.Installation
	PrivateKey   *rsa.PrivateKey
	BotUID       string
}

// NewScenario seeds Firestore with an App + bot user + Installation and
// returns a Scenario ready for use. The caller MUST defer scen.Cleanup(ctx).
func NewScenario(t *testing.T, ctx context.Context, fs *firestore.Client) *Scenario {
	t.Helper()
	store := apps.NewMemorySecretStore()

	suffix := randHex(4)
	slug := "fx-" + suffix
	owner := apps.AccountRef{ID: "owner-" + suffix, Type: apps.AccountTypeUser}

	botUID, err := apps.CreateBotUser(ctx, fs, slug, slug, "", "pending")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}

	app, secrets, err := apps.CreateApp(ctx, fs, store, apps.CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL: "https://example.test/hook",
		DefaultPermissions: apps.Permissions{
			"issues": apps.PermWrite, "contents": apps.PermWrite,
			"pull_requests": apps.PermWrite, "metadata": apps.PermRead,
		},
		DefaultEvents: []string{"issues", "issue_comment", "pull_request"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	installeeAcct := apps.AccountRef{ID: "acct-" + suffix, Type: apps.AccountTypeUser}
	inst, err := apps.CreateInstallation(ctx, fs, apps.CreateInstallationRequest{
		AppID: app.AppID, Account: installeeAcct,
		RepositorySelection: "all",
		Permissions:         app.DefaultPermissions,
		Events:              app.DefaultEvents,
	})
	if err != nil {
		t.Fatalf("CreateInstallation: %v", err)
	}

	block, _ := pem.Decode([]byte(secrets.PrivateKeyPEM))
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse pkcs1: %v", err)
	}

	return &Scenario{
		FS: fs, Store: store, App: app, Installation: inst, PrivateKey: priv, BotUID: botUID,
	}
}

// SignJWT returns a valid RS256 JWT for s.App, expiring in 5 minutes.
func (s *Scenario) SignJWT(t *testing.T) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": s.App.AppID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	str, err := tok.SignedString(s.PrivateKey)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return str
}

// MintToken issues a fresh installation token for s.Installation.
func (s *Scenario) MintToken(ctx context.Context) (string, error) {
	out, err := apps.MintInstallationToken(ctx, s.FS, s.Installation, apps.MintRequest{})
	if err != nil {
		return "", err
	}
	return out.Plaintext, nil
}

// SeedRepo creates a repository owned by the installation's account and
// switches the installation's repository_selection to "selected" with this
// one repo. Returns the repo name. Caller does NOT need to delete the repo
// (Scenario.Cleanup will handle it via the updated cleanup logic).
func (s *Scenario) SeedRepo(ctx context.Context) (string, error) {
	repoName := "fx-repo-" + randHex(4)
	if err := db.CreateRepositoryMetadata(ctx, s.FS,
		s.Installation.Account.ID, s.Installation.Account.ID,
		repoName, "", "public"); err != nil {
		return "", err
	}
	// Firestore repo doc-ID convention is "<owner>_<name>" lowercased (see db.go).
	repoID := strings.ToLower(s.Installation.Account.ID) + "_" + strings.ToLower(repoName)

	_, err := s.FS.Collection(apps.CollectionInstallations).
		Doc(s.Installation.InstallationID).
		Update(ctx, []firestore.Update{
			{Path: "repository_selection", Value: "selected"},
			{Path: "repository_ids", Value: []string{repoID}},
		})
	if err != nil {
		return "", err
	}
	s.Installation.RepositorySelection = "selected"
	s.Installation.RepositoryIDs = []string{repoID}
	return repoName, nil
}

// Cleanup deletes all seeded Firestore docs. Idempotent.
func (s *Scenario) Cleanup(ctx context.Context) {
	// Clean up any seeded repositories.
	for _, repoID := range s.Installation.RepositoryIDs {
		_, _ = s.FS.Collection("repositories").Doc(repoID).Delete(ctx)
	}
	_, _ = s.FS.Collection(apps.CollectionInstallations).Doc(s.Installation.InstallationID).Delete(ctx)
	_, _ = s.FS.Collection(apps.CollectionApps).Doc(s.App.AppID).Delete(ctx)
	_, _ = s.FS.Collection("usernames").Doc(s.App.Slug + "[bot]").Delete(ctx)
	_, _ = s.FS.Collection(apps.CollectionUsers).Doc(s.BotUID).Delete(ctx)
	// Tokens issued during the test live under installation_tokens/{hash}; we
	// leave them to Firestore TTL since their doc IDs are not tracked here.
	_ = ctx
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
