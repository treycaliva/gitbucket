package apps

import (
	"context"
	"os"
	"testing"
	"time"

	"gitbucket/internal/db"
)

func TestIsBotIdentity_SeedList(t *testing.T) {
	// Seeded sync-bot names should match without any Firestore reads.
	cache := NewBotIdentityCache(nil, time.Minute) // nil fs is OK for seed-only lookups
	if !cache.Contains("gitbucket-sync-bot") {
		t.Error("expected gitbucket-sync-bot in seed list")
	}
	if !cache.Contains("Gitbucket-Sync-Bot") {
		t.Error("expected case-insensitive match")
	}
	if cache.Contains("alice") {
		t.Error("regular user should not be a bot")
	}
}

func TestIsBotIdentity_FirestoreRefresh(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	// Create a bot user.
	slug := "bot-cache-" + randHex(4)
	botUID, err := CreateBotUser(ctx, fs, slug, slug, "", "test-app")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
	})

	cache := NewBotIdentityCache(fs, 100*time.Millisecond)
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if !cache.Contains(slug + "[bot]") {
		t.Errorf("expected %s[bot] to be recognized as a bot after refresh", slug)
	}
}

func TestIsBotIdentity_TopLevelHelper(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	// The top-level package-level cache must be settable via SetBotIdentityCache
	// so tests can inject a controlled instance and IsBotIdentity uses it.
	prev := DefaultBotIdentityCache
	defer SetDefaultBotIdentityCache(prev)

	cache := NewBotIdentityCache(nil, time.Minute)
	SetDefaultBotIdentityCache(cache)

	if !IsBotIdentity(context.Background(), "gitbucket-sync-bot") {
		t.Error("IsBotIdentity package-level helper should see seed list")
	}
}
