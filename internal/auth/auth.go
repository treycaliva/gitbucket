package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"cloud.google.com/go/firestore"
	fauth "firebase.google.com/go/v4/auth"
	"gitbucket/internal/db"
)

type contextKey string

const (
	UIDContextKey      contextKey = "uid"
	UsernameContextKey contextKey = "username"
)

// AuthHandler handles token verification.
type AuthHandler struct {
	DevMode         bool
	FirebaseAuth    *fauth.Client
	FirestoreClient *firestore.Client
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(devMode bool, firebaseAuth *fauth.Client, firestoreClient *firestore.Client) *AuthHandler {
	return &AuthHandler{
		DevMode:         devMode,
		FirebaseAuth:    firebaseAuth,
		FirestoreClient: firestoreClient,
	}
}

// GetUID retrieves the Firebase UID from the context.
func GetUID(ctx context.Context) string {
	if uid, ok := ctx.Value(UIDContextKey).(string); ok {
		return uid
	}
	return ""
}

// GetUsername retrieves the username from the context.
func GetUsername(ctx context.Context) string {
	if username, ok := ctx.Value(UsernameContextKey).(string); ok {
		return username
	}
	return ""
}

// RequireWebAuth is a middleware that requires a valid token (Bearer or mock).
func (h *AuthHandler) RequireWebAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		var token string
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = authHeader[7:]
		} else if strings.HasPrefix(authHeader, "bearer ") {
			token = authHeader[7:]
		}

		if token == "" {
			http.Error(w, "Unauthorized: missing bearer token", http.StatusUnauthorized)
			return
		}

		var uid string
		if h.DevMode && strings.HasPrefix(token, "mock_") {
			uid = strings.TrimPrefix(token, "mock_")
		} else {
			if h.FirebaseAuth == nil {
				http.Error(w, "Internal Server Error: auth client not initialized", http.StatusInternalServerError)
				return
			}
			verifiedToken, err := h.FirebaseAuth.VerifyIDToken(r.Context(), token)
			if err != nil {
				http.Error(w, "Unauthorized: invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}
			uid = verifiedToken.UID
		}

		ctx := r.Context()

		if err := db.RejectBotUID(ctx, h.FirestoreClient, uid); err != nil {
			http.Error(w, "Forbidden: "+err.Error(), http.StatusForbidden)
			return
		}

		ctx = context.WithValue(ctx, UIDContextKey, uid)

		if h.FirestoreClient != nil {
			if username, err := db.GetUsernameByUID(ctx, h.FirestoreClient, uid); err == nil && username != "" {
				ctx = context.WithValue(ctx, UsernameContextKey, username)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireUID extracts and verifies the request's authentication, returning
// the UID or an error. Used by handlers that need to gate by web auth
// (Firebase ID token or DevMode mock_<uid> token) without being in the
// RequireWebAuth middleware chain.
func (h *AuthHandler) RequireUID(r *http.Request) (string, error) {
	// Check if already set by middleware.
	if uid := GetUID(r.Context()); uid != "" {
		return uid, nil
	}
	// Parse Authorization header directly.
	authHeader := r.Header.Get("Authorization")
	var tok string
	if strings.HasPrefix(authHeader, "Bearer ") {
		tok = authHeader[7:]
	} else if strings.HasPrefix(authHeader, "bearer ") {
		tok = authHeader[7:]
	}
	if tok == "" {
		return "", fmt.Errorf("missing bearer token")
	}
	var uid string
	if h.DevMode && strings.HasPrefix(tok, "mock_") {
		uid = strings.TrimPrefix(tok, "mock_")
	} else {
		if h.FirebaseAuth == nil {
			return "", fmt.Errorf("auth client not configured")
		}
		verifiedToken, err := h.FirebaseAuth.VerifyIDToken(r.Context(), tok)
		if err != nil {
			return "", err
		}
		uid = verifiedToken.UID
	}
	if err := db.RejectBotUID(r.Context(), h.FirestoreClient, uid); err != nil {
		return "", err
	}
	return uid, nil
}

// OptionalWebAuth is a middleware that optionally authenticates the token (Bearer or mock).
func (h *AuthHandler) OptionalWebAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		var token string
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = authHeader[7:]
		} else if strings.HasPrefix(authHeader, "bearer ") {
			token = authHeader[7:]
		}

		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		var uid string
		if h.DevMode && strings.HasPrefix(token, "mock_") {
			uid = strings.TrimPrefix(token, "mock_")
		} else {
			if h.FirebaseAuth == nil {
				next.ServeHTTP(w, r)
				return
			}
			verifiedToken, err := h.FirebaseAuth.VerifyIDToken(r.Context(), token)
			if err != nil {
				http.Error(w, "Unauthorized: invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}
			uid = verifiedToken.UID
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, UIDContextKey, uid)

		if h.FirestoreClient != nil {
			if username, err := db.GetUsernameByUID(ctx, h.FirestoreClient, uid); err == nil && username != "" {
				ctx = context.WithValue(ctx, UsernameContextKey, username)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
