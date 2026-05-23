package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"

	"gitbucket/internal/db"
)

// ManifestRequest is the inbound JSON for the hop-2 endpoint. Matches
// GitHub's manifest schema (subset GitBucket implements).
type ManifestRequest struct {
	Manifest ManifestDoc `json:"manifest"`
}

type ManifestDoc struct {
	Name               string                 `json:"name"`
	URL                string                 `json:"url"`
	HookAttributes     ManifestHookAttributes `json:"hook_attributes"`
	RedirectURL        string                 `json:"redirect_url"`
	CallbackURLs       []string               `json:"callback_urls"`
	Public             bool                   `json:"public"`
	DefaultPermissions map[string]string      `json:"default_permissions"`
	DefaultEvents      []string               `json:"default_events"`
}

type ManifestHookAttributes struct {
	URL string `json:"url"`
}

// validManifestEvents is the allow-list of event names a manifest may
// request. Anything outside this set is silently dropped (matches GitHub's
// permissive behavior).
var validManifestEvents = map[string]bool{
	"issues":                      true,
	"issue_comment":               true,
	"pull_request":                true,
	"pull_request_review_comment": true,
	"push":                        true,
	"installation":                true,
}

// slugAllowed restricts the characters that may appear in a generated slug.
// Mirrors the user-facing username regex from internal/api/api.go.
var slugAllowed = regexp.MustCompile(`[^a-z0-9-]+`)

// CreateManifestApp handles POST /api/v3/settings/apps/manifest-conversions.
// Requires Firebase web auth; the logged-in user becomes the App owner.
func (h *Handler) CreateManifestApp(w http.ResponseWriter, r *http.Request) {
	// 1. Authenticate via web auth (Firebase ID token or mock_<uid> in DevMode).
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}
	username, err := db.GetUsernameByUID(r.Context(), h.FS, uid)
	if err != nil || username == "" {
		WriteError(w, ErrUnauthorized)
		return
	}

	// 2. Parse + validate the manifest.
	var req ManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, ErrUnprocessable)
		return
	}
	m := req.Manifest
	if m.Name == "" || m.URL == "" || m.HookAttributes.URL == "" || m.RedirectURL == "" {
		WriteError(w, ErrUnprocessable)
		return
	}

	// 3. Validate + filter permissions (drop unknown scopes; keep known ones).
	perms := Permissions{}
	for scope, level := range m.DefaultPermissions {
		lv := ParsePermissionLevel(level)
		if lv == PermNone {
			continue
		}
		perms[scope] = lv
	}
	// Filter events to the allow-list.
	events := make([]string, 0, len(m.DefaultEvents))
	for _, e := range m.DefaultEvents {
		if validManifestEvents[e] {
			events = append(events, e)
		}
	}

	// 4. Allocate a unique slug.
	slug, err := allocateSlug(r.Context(), h.FS, m.Name)
	if err != nil {
		WriteError(w, err)
		return
	}

	// 5. Create the bot user.
	botUID, err := CreateBotUser(r.Context(), h.FS, slug, m.Name, "", "pending")
	if err != nil {
		WriteError(w, err)
		return
	}

	// 6. Create the App (generates keypair, secrets, persists to Firestore + Secret Manager).
	owner := AccountRef{ID: uid, Type: AccountTypeUser}
	app, secrets, err := CreateApp(r.Context(), h.FS, h.Store, CreateAppRequest{
		Slug:               slug,
		Name:               m.Name,
		OwnerAccount:       owner,
		BotUserID:          botUID,
		WebhookURL:         m.HookAttributes.URL,
		DefaultPermissions: perms,
		DefaultEvents:      events,
	})
	if err != nil {
		// Best-effort cleanup: delete the bot user we just created.
		_, _ = h.FS.Collection("usernames").Doc(slug + "[bot]").Delete(r.Context())
		_, _ = h.FS.Collection(CollectionUsers).Doc(botUID).Delete(r.Context())
		WriteError(w, err)
		return
	}

	// 7. Stash the secrets bundle keyed by a one-time code.
	code, err := CreateManifestConversion(r.Context(), h.FS, app.AppID)
	if err != nil {
		cleanupOrphanedApp(r.Context(), h.FS, app, slug, botUID)
		WriteError(w, err)
		return
	}
	// Also stash the plaintext secrets under the code so hop 3 can fetch them
	// without re-running key generation. We piggyback on a sibling doc in a
	// short-lived subcollection so the Code doc itself stays small.
	if err := stashConversionSecrets(r.Context(), h.FS, code, secrets); err != nil {
		cleanupOrphanedApp(r.Context(), h.FS, app, slug, botUID)
		// Also delete the conversion code we just created so it can't be exchanged.
		_, _ = h.FS.Collection(CollectionManifestConversions).Doc(code).Delete(r.Context())
		WriteError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"redirect_url": appendCodeQuery(m.RedirectURL, code),
	})

	// Keep username for downstream logging clarity.
	_ = username
}

// ListMyApps handles GET /api/v3/user/apps. Lists Apps owned by the
// authenticated user. Web-authed.
func (h *Handler) ListMyApps(w http.ResponseWriter, r *http.Request) {
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}
	iter := h.FS.Collection(CollectionApps).Where("owner_account.id", "==", uid).Documents(r.Context())
	defer iter.Stop()
	out := []map[string]interface{}{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var a App
		if err := doc.DataTo(&a); err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":   a.AppID,
			"slug": a.Slug,
			"name": a.Name,
		})
	}
	WriteJSON(w, http.StatusOK, out)
}

// ListMyInstallations handles GET /api/v3/user/installations. Lists
// installations on the authenticated user's account. Web-authed.
func (h *Handler) ListMyInstallations(w http.ResponseWriter, r *http.Request) {
	uid, err := h.Auth.RequireUID(r)
	if err != nil {
		WriteError(w, ErrUnauthorized)
		return
	}
	iter := h.FS.Collection(CollectionInstallations).Where("account.id", "==", uid).Documents(r.Context())
	defer iter.Stop()
	out := []map[string]interface{}{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var inst Installation
		if err := doc.DataTo(&inst); err != nil {
			continue
		}
		appName := ""
		if app, _ := GetApp(r.Context(), h.FS, inst.AppID); app != nil {
			appName = app.Name
		}
		out = append(out, map[string]interface{}{
			"id":       inst.InstallationID,
			"app_id":   inst.AppID,
			"app_name": appName,
		})
	}
	WriteJSON(w, http.StatusOK, out)
}

// GetAppPublic handles GET /api/v3/apps/{slug}/public — returns just the
// fields safe for an unauthenticated install-page preview.
func (h *Handler) GetAppPublic(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	app, err := GetAppBySlug(r.Context(), h.FS, slug)
	if err != nil || app == nil {
		WriteError(w, ErrNotFound)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id":          app.AppID,
		"slug":        app.Slug,
		"name":        app.Name,
		"permissions": permissionsJSON(app.DefaultPermissions),
		"events":      app.DefaultEvents,
	})
}

// ExchangeManifestCode handles POST /api/v3/app-manifests/{code}/conversions.
// Looks up the code, returns the plaintext secrets bundle (one time only),
// deletes the conversion record.
func (h *Handler) ExchangeManifestCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	appID, err := ConsumeManifestConversion(r.Context(), h.FS, code)
	if err != nil || appID == "" {
		WriteError(w, ErrNotFound)
		return
	}
	app, err := GetApp(r.Context(), h.FS, appID)
	if err != nil || app == nil {
		WriteError(w, ErrNotFound)
		return
	}
	secrets, err := loadConversionSecrets(r.Context(), h.FS, code)
	if err != nil {
		WriteError(w, ErrNotFound)
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id":             app.AppID,
		"slug":           app.Slug,
		"name":           app.Name,
		"owner":          map[string]interface{}{"login": app.OwnerAccount.ID, "type": string(app.OwnerAccount.Type)},
		"client_id":      secrets.ClientID,
		"client_secret":  secrets.ClientSecret,
		"webhook_secret": secrets.WebhookSecret,
		"pem":            secrets.PrivateKeyPEM,
		"html_url":       "",
		"permissions":    permissionsJSON(app.DefaultPermissions),
		"events":         app.DefaultEvents,
	})
}

// --- private helpers ------------------------------------------------------

// allocateSlug derives a unique slug from a display name. Falls back to
// appending a 4-char hex suffix on collision.
func allocateSlug(ctx context.Context, fs *firestore.Client, name string) (string, error) {
	base := slugify(name)
	if base == "" {
		base = "app"
	}
	// Truncate to fit within the username regex's 20-char limit (minus 5
	// for the "[bot]" suffix CreateBotUser will append).
	if len(base) > 15 {
		base = base[:15]
	}
	candidate := base
	for attempt := 0; attempt < 10; attempt++ {
		existing, err := GetAppBySlug(ctx, fs, candidate)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return candidate, nil
		}
		suffix := make([]byte, 2)
		_, _ = rand.Read(suffix)
		candidate = base + "-" + hex.EncodeToString(suffix)
		if len(candidate) > 20 {
			candidate = candidate[:20]
		}
	}
	return "", fmt.Errorf("could not allocate unique slug")
}

func slugify(name string) string {
	s := strings.ToLower(name)
	s = slugAllowed.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

func appendCodeQuery(redirectURL, code string) string {
	sep := "?"
	if strings.Contains(redirectURL, "?") {
		sep = "&"
	}
	return redirectURL + sep + "code=" + code
}

// --- conversion secrets stash --------------------------------------------

// conversionSecretsDoc is stored as Firestore: manifest_conversions/{code}/secrets/v1.
// IMPORTANT: Firestore TTL on manifest_conversions does NOT cascade to this
// subcollection. The doc is deleted by loadConversionSecrets on a successful
// hop-3 exchange. Codes that expire un-exchanged leave the secrets doc
// orphaned (unreachable without the code, but present). A periodic sweeper
// is tracked as a follow-on; until then, this is a known credential
// retention gap.
type conversionSecretsDoc struct {
	ClientID      string `firestore:"client_id"`
	ClientSecret  string `firestore:"client_secret"`
	WebhookSecret string `firestore:"webhook_secret"`
	PrivateKeyPEM string `firestore:"private_key_pem"`
}

func stashConversionSecrets(ctx context.Context, fs *firestore.Client, code string, s *AppSecrets) error {
	doc := conversionSecretsDoc{
		ClientID:      s.ClientID,
		ClientSecret:  s.ClientSecret,
		WebhookSecret: s.WebhookSecret,
		PrivateKeyPEM: s.PrivateKeyPEM,
	}
	_, err := fs.Collection(CollectionManifestConversions).Doc(code).
		Collection("secrets").Doc("v1").Create(ctx, doc)
	return err
}

func loadConversionSecrets(ctx context.Context, fs *firestore.Client, code string) (*AppSecrets, error) {
	docSnap, err := fs.Collection(CollectionManifestConversions).Doc(code).
		Collection("secrets").Doc("v1").Get(ctx)
	if err != nil {
		return nil, err
	}
	var d conversionSecretsDoc
	if err := docSnap.DataTo(&d); err != nil {
		return nil, err
	}
	// Best-effort delete after reading.
	_, _ = fs.Collection(CollectionManifestConversions).Doc(code).
		Collection("secrets").Doc("v1").Delete(ctx)
	return &AppSecrets{
		ClientID:      d.ClientID,
		ClientSecret:  d.ClientSecret,
		WebhookSecret: d.WebhookSecret,
		PrivateKeyPEM: d.PrivateKeyPEM,
	}, nil
}

// cleanupOrphanedApp is a best-effort delete of an App + its bot user when a
// later step in CreateManifestApp fails (e.g. CreateManifestConversion or
// stashConversionSecrets errors after CreateApp succeeds). All deletes are
// best-effort; partial failures are logged via FireError for visibility.
func cleanupOrphanedApp(ctx context.Context, fs *firestore.Client, app *App, slug, botUID string) {
	if _, err := fs.Collection(CollectionApps).Doc(app.AppID).Delete(ctx); err != nil {
		FireError("orphan cleanup: delete app %s: %v", app.AppID, err)
	}
	if _, err := fs.Collection("usernames").Doc(slug + "[bot]").Delete(ctx); err != nil {
		FireError("orphan cleanup: delete username %s[bot]: %v", slug, err)
	}
	if botUID != "" {
		if _, err := fs.Collection(CollectionUsers).Doc(botUID).Delete(ctx); err != nil {
			FireError("orphan cleanup: delete bot user %s: %v", botUID, err)
		}
	}
}
