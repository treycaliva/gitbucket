// Package apps implements GitHub App emulation: registration, JWT verification,
// installation token issuance, request authentication, and webhook fan-out.
// Spec: docs/superpowers/specs/2026-05-20-github-app-emulation-design.md
package apps

import "time"

// PermissionLevel models GitHub App permission grants. Order matters: higher
// levels satisfy lower ones (Write satisfies Read).
type PermissionLevel int

const (
	PermNone PermissionLevel = iota
	PermRead
	PermWrite
)

func (p PermissionLevel) String() string {
	switch p {
	case PermWrite:
		return "write"
	case PermRead:
		return "read"
	default:
		return "none"
	}
}

func ParsePermissionLevel(s string) PermissionLevel {
	switch s {
	case "write":
		return PermWrite
	case "read":
		return PermRead
	default:
		return PermNone
	}
}

// Permissions is the map form used in JSON I/O and on Firestore docs.
type Permissions map[string]PermissionLevel

func (p Permissions) Satisfies(scope string, need PermissionLevel) bool {
	got, ok := p[scope]
	if !ok {
		return false
	}
	return got >= need
}

// AccountType discriminates the polymorphic account reference. MVP only emits
// "User"; "Organization" is reserved for a follow-on spec.
type AccountType string

const (
	AccountTypeUser AccountType = "User"
	AccountTypeOrg  AccountType = "Organization"
)

type AccountRef struct {
	ID   string      `firestore:"id" json:"id"`
	Type AccountType `firestore:"type" json:"type"`
}

// App is the Firestore-persisted representation of a registered GitHub App.
// See spec §4 for field meanings. Secret values themselves live in Secret
// Manager — only their resource names are stored here.
type App struct {
	AppID                 string      `firestore:"app_id"`
	Slug                  string      `firestore:"slug"`
	Name                  string      `firestore:"name"`
	OwnerAccount          AccountRef  `firestore:"owner_account"`
	BotUserID             string      `firestore:"bot_user_id"`
	ClientID              string      `firestore:"client_id"`
	ClientSecretHash      string      `firestore:"client_secret_hash"`
	WebhookURL            string      `firestore:"webhook_url"`
	WebhookSecretResource string      `firestore:"webhook_secret_resource"`
	WebhookSecretHash     string      `firestore:"webhook_secret_hash"`
	PrivateKeySecret      string      `firestore:"private_key_secret"`
	PublicKeyPEM          string      `firestore:"public_key_pem"`
	DefaultPermissions    Permissions `firestore:"default_permissions"`
	DefaultEvents         []string    `firestore:"default_events"`
	SuspendedAt           *time.Time  `firestore:"suspended_at"`
	CreatedAt             time.Time   `firestore:"created_at"`
}

// Installation links an App to an account (user, in MVP) and an optional
// subset of repository IDs.
type Installation struct {
	InstallationID      string      `firestore:"installation_id"`
	AppID               string      `firestore:"app_id"`
	Account             AccountRef  `firestore:"account"`
	RepositorySelection string      `firestore:"repository_selection"` // "all" | "selected"
	RepositoryIDs       []string    `firestore:"repository_ids"`
	Permissions         Permissions `firestore:"permissions"`
	Events              []string    `firestore:"events"`
	SuspendedAt         *time.Time  `firestore:"suspended_at"`
	CreatedAt           time.Time   `firestore:"created_at"`
}

// InstallationToken is the Firestore record for a minted access token. The
// document ID is the sha256 hex of the token plaintext — never the plaintext
// itself.
type InstallationToken struct {
	InstallationID string      `firestore:"installation_id"`
	Permissions    Permissions `firestore:"permissions"`
	RepositoryIDs  []string    `firestore:"repository_ids"`
	IssuedAt       time.Time   `firestore:"issued_at"`
	ExpiresAt      time.Time   `firestore:"expires_at"`
}

func (t *InstallationToken) Expired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

// Firestore collection names. Kept centralized so tests and seed scripts agree.
const (
	CollectionApps               = "apps"
	CollectionInstallations      = "installations"
	CollectionInstallationTokens = "installation_tokens"
	CollectionUsers              = "users" // existing collection; bot users live here too
)
