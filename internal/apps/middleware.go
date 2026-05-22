// internal/apps/middleware.go
package apps

import (
	"context"
	"net/http"
	"strings"

	"cloud.google.com/go/firestore"
)

// InstallationContext is injected into the request context by
// RequireInstallationToken. Handlers read it via InstallationContextFrom.
type InstallationContext struct {
	InstallationID string
	AppID          string
	AppSlug        string
	Account        AccountRef
	Permissions    Permissions
	RepositoryIDs  []string
	BotUserID      string
}

type installationCtxKey struct{}

func InstallationContextFrom(ctx context.Context) *InstallationContext {
	c, _ := ctx.Value(installationCtxKey{}).(*InstallationContext)
	return c
}

func WithInstallationContext(ctx context.Context, c *InstallationContext) context.Context {
	return context.WithValue(ctx, installationCtxKey{}, c)
}

// RequireInstallationToken parses a `ghs_`-prefixed token from Authorization,
// the X-Access-Token header, or HTTP Basic (`x-access-token:<token>`), verifies
// it, and populates InstallationContext on the request.
func RequireInstallationToken(fs *firestore.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractInstallationToken(r)
			if tok == "" {
				WriteError(w, ErrUnauthorized)
				return
			}
			rec, err := VerifyInstallationToken(r.Context(), fs, tok)
			if err != nil || rec == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			inst, err := GetInstallation(r.Context(), fs, rec.InstallationID)
			if err != nil || inst == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			// Read the App row for app_id + bot_user_id. This is one extra
			// point-read per request; a follow-on can cache by app_id with TTL.
			app, err := GetApp(r.Context(), fs, inst.AppID)
			if err != nil || app == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			ic := &InstallationContext{
				InstallationID: inst.InstallationID,
				AppID:          inst.AppID,
				AppSlug:        app.Slug,
				Account:        inst.Account,
				Permissions:    rec.Permissions,
				RepositoryIDs:  rec.RepositoryIDs,
				BotUserID:      app.BotUserID,
			}
			next.ServeHTTP(w, r.WithContext(WithInstallationContext(r.Context(), ic)))
		})
	}
}

// RequirePerm returns nil if the installation context grants `need` on `scope`.
// Returns *PermissionError otherwise, which WriteError maps to 403 with the
// GitHub-shape body "Resource not accessible by integration".
func RequirePerm(ctx context.Context, scope string, need PermissionLevel) error {
	ic := InstallationContextFrom(ctx)
	if ic == nil {
		return ErrUnauthorized
	}
	if !ic.Permissions.Satisfies(scope, need) {
		return &PermissionError{Scope: scope, Need: need}
	}
	return nil
}

func extractInstallationToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		v := strings.TrimSpace(h[len("Bearer "):])
		if strings.HasPrefix(v, tokenPrefix) {
			return v
		}
	}
	if h := r.Header.Get("X-Access-Token"); strings.HasPrefix(h, tokenPrefix) {
		return h
	}
	// HTTP Basic: user = "x-access-token", pass = <token>. Git CLI uses this.
	user, pass, ok := r.BasicAuth()
	if ok && user == "x-access-token" && strings.HasPrefix(pass, tokenPrefix) {
		return pass
	}
	return ""
}
