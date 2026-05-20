package auth

import (
	"context"
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
		ctx = context.WithValue(ctx, UIDContextKey, uid)

		if h.FirestoreClient != nil {
			if username, err := db.GetUsernameByUID(ctx, h.FirestoreClient, uid); err == nil && username != "" {
				ctx = context.WithValue(ctx, UsernameContextKey, username)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
