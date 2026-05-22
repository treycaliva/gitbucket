// internal/apps/jwt_middleware.go
package apps

import (
	"context"
	"net/http"
	"strings"
)

type appCtxKey struct{}

func AppFromContext(ctx context.Context) *App {
	a, _ := ctx.Value(appCtxKey{}).(*App)
	return a
}

func WithApp(ctx context.Context, a *App) context.Context {
	return context.WithValue(ctx, appCtxKey{}, a)
}

// RequireAppJWT mounts as chi middleware: verifies a Bearer JWT and injects
// the issuing App into the request context. Used for routes like
// `POST /api/v3/app/installations/{id}/access_tokens` that authenticate with
// an App JWT (not an installation token).
func RequireAppJWT(v *JWTVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractBearer(r)
			if tok == "" {
				WriteError(w, ErrUnauthorized)
				return
			}
			app, err := v.Verify(r.Context(), tok)
			if err != nil || app == nil {
				WriteError(w, ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithApp(r.Context(), app)))
		})
	}
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[len("Bearer "):])
}
