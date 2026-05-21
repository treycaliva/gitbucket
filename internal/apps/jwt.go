package apps

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/golang-jwt/jwt/v4"
)

// errInvalidJWT is the single sentinel returned for all Verify failures.
// Callers (and HTTP clients) never learn which check failed — see logs for detail.
var errInvalidJWT = errors.New("invalid jwt")

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
	// ParseUnverified to extract `iss` before we know which pubkey to use.
	// Use a parser that skips claims validation so we are not subject to the
	// library's zero-leeway exp check during this step.
	unverifiedParser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Name}))
	parsed, _, err := unverifiedParser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		log.Printf("[apps.jwt] reject: malformed token: %v", err)
		return nil, errInvalidJWT
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		log.Printf("[apps.jwt] reject: claims not MapClaims")
		return nil, errInvalidJWT
	}
	iss, _ := claims["iss"].(string)
	if iss == "" {
		log.Printf("[apps.jwt] reject: missing iss")
		return nil, errInvalidJWT
	}

	entry, err := v.loadEntry(ctx, iss)
	if err != nil || entry == nil {
		log.Printf("[apps.jwt] reject: unknown issuer %q: %v", iss, err)
		return nil, errInvalidJWT
	}
	if entry.app.SuspendedAt != nil {
		log.Printf("[apps.jwt] reject: app %s is suspended", iss)
		return nil, errInvalidJWT
	}

	// Verify signature only. SkipClaimsValidation=true bypasses the library's
	// zero-leeway exp/iat check so our manual checks below (with jwtClockSkew)
	// are not dead code — the library would otherwise reject expired-by-1s tokens
	// before we ever reach the grace-window logic.
	sigParser := &jwt.Parser{
		ValidMethods:         []string{jwt.SigningMethodRS256.Name},
		SkipClaimsValidation: true,
	}
	_, err = sigParser.Parse(tokenStr, func(_ *jwt.Token) (interface{}, error) {
		return entry.pubKey, nil
	})
	if err != nil {
		log.Printf("[apps.jwt] reject: signature invalid for app %s: %v", iss, err)
		return nil, errInvalidJWT
	}

	now := time.Now()
	iatF, _ := claims["iat"].(float64)
	expF, _ := claims["exp"].(float64)
	if iatF == 0 || expF == 0 {
		log.Printf("[apps.jwt] reject: missing iat/exp for app %s", iss)
		return nil, errInvalidJWT
	}
	iat := time.Unix(int64(iatF), 0)
	exp := time.Unix(int64(expF), 0)

	// iat must not be in the future (beyond clock skew tolerance).
	if iat.After(now.Add(jwtClockSkew)) {
		log.Printf("[apps.jwt] reject: iat in future for app %s", iss)
		return nil, errInvalidJWT
	}
	// exp must not be in the past (beyond clock skew tolerance).
	// This is the live check — the library's check is skipped above so this leeway
	// is real and not dead code.
	if exp.Before(now.Add(-jwtClockSkew)) {
		log.Printf("[apps.jwt] reject: token expired for app %s", iss)
		return nil, errInvalidJWT
	}
	// Enforce maximum lifetime window (library does not check this).
	if exp.Sub(iat) > jwtMaxExpWindow+jwtClockSkew {
		log.Printf("[apps.jwt] reject: exp window too wide for app %s", iss)
		return nil, errInvalidJWT
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
