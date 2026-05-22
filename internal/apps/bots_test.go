package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"gitbucket/internal/db"
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestBotUsernameValidator(t *testing.T) {
	tooLong := make([]byte, 21)
	for i := range tooLong {
		tooLong[i] = 'a'
	}
	cases := []struct {
		slug string
		want string
		ok   bool
	}{
		{"claude-code", "claude-code[bot]", true},
		{"a", "", false},             // too short (1 char)
		{"ab", "", false},            // too short (2 chars, fence-post)
		{"abc", "abc[bot]", true},   // exactly 3 chars (minimum valid)
		{strings.Repeat("a", 20), strings.Repeat("a", 20) + "[bot]", true}, // exactly 20 chars (maximum valid)
		{string(tooLong), "", false}, // too long (21 chars)
		{"bad!", "", false},          // invalid chars
	}
	for _, c := range cases {
		got, err := BotUsernameForSlug(c.slug)
		if c.ok && err != nil {
			t.Errorf("BotUsernameForSlug(%q) unexpected error: %v", c.slug, err)
		}
		if !c.ok && err == nil {
			t.Errorf("BotUsernameForSlug(%q) expected error, got %q", c.slug, got)
		}
		if c.ok && got != c.want {
			t.Errorf("BotUsernameForSlug(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

func TestIsBotUsername(t *testing.T) {
	if !IsBotUsername("claude-code[bot]") {
		t.Error("claude-code[bot] should be a bot username")
	}
	if IsBotUsername("claude-code") {
		t.Error("claude-code should not be a bot username")
	}
	// fence-post: minimum valid slug (3 chars)
	if !IsBotUsername("abc[bot]") {
		t.Error("abc[bot] should be a bot username (minimum valid slug)")
	}
	// slug too short (2 chars) → not a bot username
	if IsBotUsername("ab[bot]") {
		t.Error("ab[bot] should not be a bot username (slug too short)")
	}
	// uppercase slug — slugRegex allows uppercase
	if !IsBotUsername("CLAUDE[bot]") {
		t.Error("CLAUDE[bot] should be a bot username (uppercase slug is valid)")
	}
	// empty string
	if IsBotUsername("") {
		t.Error("empty string should not be a bot username")
	}
}

func TestCreateBotUser(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()

	slug := "test-bot-" + randHex(4)
	var uid string
	t.Cleanup(func() {
		cctx := context.Background()
		if uid != "" {
			_, _ = fs.Collection(CollectionUsers).Doc(uid).Delete(cctx)
		}
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
	})

	uid, err = CreateBotUser(ctx, fs, slug, "Test Bot", "https://example.com/a.png", "app-xyz")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}
	if uid == "" {
		t.Fatal("expected non-empty uid")
	}

	doc, err := fs.Collection(CollectionUsers).Doc(uid).Get(ctx)
	if err != nil {
		t.Fatalf("read back bot user: %v", err)
	}
	data := doc.Data()
	if data["username"] != slug+"[bot]" {
		t.Errorf("username = %v, want %s[bot]", data["username"], slug)
	}
	if data["type"] != "Bot" {
		t.Errorf("type = %v, want Bot", data["type"])
	}
	if data["owning_app_id"] != "app-xyz" {
		t.Errorf("owning_app_id = %v, want app-xyz", data["owning_app_id"])
	}
}
