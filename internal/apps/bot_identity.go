package apps

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// botSeedList is the baked-in list of sync-bot usernames that must always be
// treated as bot identities even before the Firestore-backed cache loads.
// Preserves the behavior of the old hard-coded knownSyncIdentities map in
// internal/api/webhooks.go.
var botSeedList = []string{
	"gitbucket-sync-bot",
	"gitbucket-sync-bot[bot]",
	"jules-sync-bot",
	"github-actions[bot]",
}

// BotIdentityCache is a snapshot of bot usernames refreshed periodically from
// Firestore. Reads are lock-free via an atomic.Value swap of the set.
type BotIdentityCache struct {
	fs           *firestore.Client
	refreshEvery time.Duration
	set          atomic.Value // map[string]struct{} (lowercased keys)

	refreshOnce sync.Mutex // serializes concurrent Refresh() calls
}

// NewBotIdentityCache constructs an initially-seed-populated cache. If fs is
// non-nil, callers should invoke Refresh() periodically (or Start() to spawn
// a background refresher).
func NewBotIdentityCache(fs *firestore.Client, refreshEvery time.Duration) *BotIdentityCache {
	c := &BotIdentityCache{fs: fs, refreshEvery: refreshEvery}
	seed := make(map[string]struct{}, len(botSeedList))
	for _, name := range botSeedList {
		seed[strings.ToLower(name)] = struct{}{}
	}
	c.set.Store(seed)
	return c
}

// Contains reports whether the given login matches a known bot identity.
// Case-insensitive.
func (c *BotIdentityCache) Contains(login string) bool {
	set := c.set.Load().(map[string]struct{})
	_, ok := set[strings.ToLower(login)]
	return ok
}

// Refresh queries Firestore for every users/{id} doc with type:"Bot" and
// rebuilds the set, retaining the seed list. Safe to call concurrently.
func (c *BotIdentityCache) Refresh(ctx context.Context) error {
	if c.fs == nil {
		return nil // seed-only mode (tests)
	}
	c.refreshOnce.Lock()
	defer c.refreshOnce.Unlock()

	next := make(map[string]struct{}, len(botSeedList)+16)
	for _, name := range botSeedList {
		next[strings.ToLower(name)] = struct{}{}
	}

	iter := c.fs.Collection(CollectionUsers).Where("type", "==", "Bot").Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		login, _ := data["username"].(string)
		if login != "" {
			next[strings.ToLower(login)] = struct{}{}
		}
	}
	c.set.Store(next)
	return nil
}

// Start spawns a goroutine that calls Refresh on the configured interval
// until ctx is cancelled. Safe to call multiple times — each invocation
// spawns its own goroutine, so call exactly once at startup.
func (c *BotIdentityCache) Start(ctx context.Context) {
	if c.fs == nil {
		return
	}
	go func() {
		// Initial refresh.
		_ = c.Refresh(ctx)
		t := time.NewTicker(c.refreshEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := c.Refresh(ctx); err != nil {
					logEvents("bot-identity refresh: " + err.Error())
				}
			}
		}
	}()
}

// --- Package-level singleton for use by webhook receivers ------------------

// DefaultBotIdentityCache is the package-level cache instance consulted by
// IsBotIdentity. Set via SetDefaultBotIdentityCache during main() bootstrap.
// Until set, Contains() on the seed list still works (Default is a fresh
// seed-only cache).
var DefaultBotIdentityCache = NewBotIdentityCache(nil, 60*time.Second)

// SetDefaultBotIdentityCache swaps in a cache backed by a real Firestore
// client. Call once during main() init, after the bot-user records have been
// migrated to type:"Bot".
func SetDefaultBotIdentityCache(c *BotIdentityCache) {
	DefaultBotIdentityCache = c
}

// IsBotIdentity is the public helper that webhook receivers (and the events
// Fire path) call. Reads from DefaultBotIdentityCache.
func IsBotIdentity(_ context.Context, login string) bool {
	return DefaultBotIdentityCache.Contains(login)
}
