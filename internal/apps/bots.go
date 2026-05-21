package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// slugRegex matches the slug part of a bot username (3-20 chars, alnum + hyphen).
// Same character set as the existing user-facing username regex; the `[bot]`
// suffix is appended by the App-registration path only.
var slugRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{3,20}$`)

// botUsernameRegex matches a full bot username, e.g. "claude-code[bot]".
var botUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{3,20}\[bot\]$`)

// BotUsernameForSlug returns "<slug>[bot]" if slug is valid, else an error.
func BotUsernameForSlug(slug string) (string, error) {
	if !slugRegex.MatchString(slug) {
		return "", fmt.Errorf("invalid slug %q (must match %s)", slug, slugRegex.String())
	}
	return slug + "[bot]", nil
}

// IsBotUsername returns true for usernames ending in `[bot]` and matching the
// reserved shape. Used by the user resolver to reject login / PAT writes for
// bot accounts.
func IsBotUsername(username string) bool {
	return botUsernameRegex.MatchString(username)
}

// CreateBotUser persists a synthetic bot user in Firestore and returns its UID.
// Also writes the usernames/{bot-username} → {uid} mapping so existing lookups
// (e.g. GetUidByUsername) work for bot logins.
func CreateBotUser(ctx context.Context, fs *firestore.Client, slug, displayName, avatarURL, owningAppID string) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("firestore client is nil")
	}
	botUsername, err := BotUsernameForSlug(slug)
	if err != nil {
		return "", err
	}

	uidBytes := make([]byte, 16)
	if _, err := rand.Read(uidBytes); err != nil {
		return "", fmt.Errorf("generate bot uid: %w", err)
	}
	uid := "bot_" + hex.EncodeToString(uidBytes)

	usernameRef := fs.Collection("usernames").Doc(botUsername)
	userRef := fs.Collection(CollectionUsers).Doc(uid)

	err = fs.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(usernameRef)
		if err == nil && doc.Exists() {
			return fmt.Errorf("bot username %q already taken", botUsername)
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		if err := tx.Set(usernameRef, map[string]interface{}{"uid": uid}); err != nil {
			return err
		}
		return tx.Set(userRef, map[string]interface{}{
			"uid":           uid,
			"username":      botUsername,
			"display_name":  displayName,
			"email":         fmt.Sprintf("%s@bots.gitbucket.local", strings.ToLower(botUsername)),
			"avatar_url":    avatarURL,
			"type":          "Bot",
			"owning_app_id": owningAppID,
			"created_at":    time.Now().UTC(),
		})
	})
	if err != nil {
		return "", err
	}
	return uid, nil
}
