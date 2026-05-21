package apps

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/golang-jwt/jwt/v4"
)

// JWTVerifier verifies App-signed JWTs against per-app public keys cached
// in-process. Cache TTL is enforced via the cacheTTL passed to NewJWTVerifier.
// Spec §5.1.
type JWTVerifier struct {
	fs       *firestore.Client
	cacheTTL time.Duration

	mu    sync.RWMutex
	cache map[string]jwtCacheEntry
}

type jwtCacheEntry struct {
	app       *App
	pubKey    *rsa.PublicKey
	expiresAt time.Time
}

const (
	jwtClockSkew    = 30 * time.Second
	jwtMaxExpWindow = 10 * time.Minute
)

func NewJWTVerifier(fs *firestore.Client, cacheTTL time.Duration) *JWTVerifier {
	return &JWTVerifier{
		fs:       fs,
		cacheTTL: cacheTTL,
		cache:    make(map[string]jwtCacheEntry),
	}
}

// Verify parses tokenStr, looks up the issuing App, verifies the signature and
// time-bound claims, and returns the App on success. Errors are intentionally
// non-specific (no leaks about which check failed) and all map to 401 at the
// HTTP layer.
func (v *JWTVerifier) Verify(ctx context.Context, tokenStr string) (*App, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Name}))
	// First parse claims without verifying signature to extract `iss`.
	parsed, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("invalid jwt")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid jwt claims")
	}
	iss, _ := claims["iss"].(string)
	if iss == "" {
		return nil, fmt.Errorf("missing iss")
	}

	entry, err := v.loadEntry(ctx, iss)
	if err != nil || entry == nil {
		return nil, fmt.Errorf("invalid jwt")
	}
	if entry.app.SuspendedAt != nil {
		return nil, fmt.Errorf("app suspended")
	}

	// Now verify signature with the cached pubkey.
	_, err = parser.Parse(tokenStr, func(_ *jwt.Token) (interface{}, error) {
		return entry.pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid jwt signature")
	}

	now := time.Now()
	iatF, _ := claims["iat"].(float64)
	expF, _ := claims["exp"].(float64)
	if iatF == 0 || expF == 0 {
		return nil, fmt.Errorf("missing iat/exp")
	}
	iat := time.Unix(int64(iatF), 0)
	exp := time.Unix(int64(expF), 0)

	if iat.After(now.Add(jwtClockSkew)) {
		return nil, fmt.Errorf("iat in future")
	}
	if exp.Before(now.Add(-jwtClockSkew)) {
		return nil, fmt.Errorf("exp in past")
	}
	if exp.Sub(iat) > jwtMaxExpWindow+jwtClockSkew {
		return nil, fmt.Errorf("exp window too wide")
	}
	return entry.app, nil
}

func (v *JWTVerifier) InvalidateCache(appID string) {
	v.mu.Lock()
	delete(v.cache, appID)
	v.mu.Unlock()
}

func (v *JWTVerifier) loadEntry(ctx context.Context, appID string) (*jwtCacheEntry, error) {
	v.mu.RLock()
	e, ok := v.cache[appID]
	v.mu.RUnlock()
	if ok && time.Now().Before(e.expiresAt) {
		return &e, nil
	}

	app, err := GetApp(ctx, v.fs, appID)
	if err != nil || app == nil {
		return nil, err
	}
	pubKey, err := parseRSAPublicKey(app.PublicKeyPEM)
	if err != nil {
		return nil, err
	}
	entry := jwtCacheEntry{
		app:       app,
		pubKey:    pubKey,
		expiresAt: time.Now().Add(v.cacheTTL),
	}
	v.mu.Lock()
	v.cache[appID] = entry
	v.mu.Unlock()
	return &entry, nil
}

func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("decode pem")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an rsa public key")
	}
	return rsaPub, nil
}
