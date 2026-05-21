package apps

import (
	"context"
	"sync"
	"time"
)

// WebhookSecretCache is a per-process in-memory cache for App webhook
// secrets fetched from a SecretStore. Reduces Secret Manager hits on the
// webhook fan-out hot path. Cache TTL matches spec §7.2 (5 minutes).
type WebhookSecretCache struct {
	store SecretStore
	ttl   time.Duration

	mu sync.RWMutex
	m  map[string]secretCacheEntry
}

type secretCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

func NewWebhookSecretCache(store SecretStore, ttl time.Duration) *WebhookSecretCache {
	return &WebhookSecretCache{
		store: store,
		ttl:   ttl,
		m:     make(map[string]secretCacheEntry),
	}
}

// Get fetches the secret for the given resource name, hitting the cache
// when fresh. Returns a defensive copy on every call.
func (c *WebhookSecretCache) Get(ctx context.Context, resourceName string) ([]byte, error) {
	c.mu.RLock()
	e, ok := c.m[resourceName]
	c.mu.RUnlock()
	if ok && time.Now().Before(e.expiresAt) {
		cp := make([]byte, len(e.value))
		copy(cp, e.value)
		return cp, nil
	}

	v, err := c.store.Get(ctx, resourceName)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	stored := make([]byte, len(v))
	copy(stored, v)
	c.m[resourceName] = secretCacheEntry{value: stored, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

// Invalidate drops a cached entry; useful when a secret is rotated.
func (c *WebhookSecretCache) Invalidate(resourceName string) {
	c.mu.Lock()
	delete(c.m, resourceName)
	c.mu.Unlock()
}
