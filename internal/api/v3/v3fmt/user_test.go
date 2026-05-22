package v3fmt

import (
	"encoding/json"
	"os"
	"testing"
)

// staticUser is a test-only UserSource implementation.
type staticUser struct {
	login, uid, typ, avatar string
}

func (s staticUser) GetLogin() string     { return s.login }
func (s staticUser) GetUserKey() string   { return s.uid }
func (s staticUser) GetType() string      { return s.typ }
func (s staticUser) GetAvatarURL() string { return s.avatar }

func TestUserFormatter_GoldenFixture(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := User(staticUser{
		login: "alice",
		uid:   "user:alice",
		typ:   "User",
	}, urls)

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want, err := os.ReadFile("testdata/user.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	gotStr := string(gotJSON)
	wantStr := string(want)
	// Tolerate trailing newline differences from text editors.
	if gotStr != wantStr && gotStr+"\n" != wantStr {
		t.Errorf("user formatter drift.\nGot:\n%s\n\nWant:\n%s", gotStr, wantStr)
	}
}

func TestUserFormatter_BotType(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := User(staticUser{login: "claude-code[bot]", uid: "user:bot_claude", typ: "Bot"}, urls)
	if got.Type != "Bot" {
		t.Errorf("Type = %q, want Bot", got.Type)
	}
	if got.Login != "claude-code[bot]" {
		t.Errorf("Login = %q", got.Login)
	}
}

func TestUserFromMap(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	m := map[string]interface{}{
		"username":   "bob",
		"uid":        "user:bob",
		"type":       "User",
		"avatar_url": "https://gitbucket.example.com/avatars/bob",
	}
	got := UserFromMap(m, urls)
	if got.Login != "bob" || got.Type != "User" {
		t.Errorf("UserFromMap: %+v", got)
	}
}

func TestUserFromBot(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := UserFromBot("appid-xyz", "botuid-abc", urls)
	if got.Type != "Bot" {
		t.Errorf("Type = %q, want Bot", got.Type)
	}
	if got.Login != "app-appid-xyz[bot]" {
		t.Errorf("Login = %q, want app-appid-xyz[bot]", got.Login)
	}
}

func TestUserAvatarDefault(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := User(staticUser{login: "alice", uid: "user:alice", typ: "User", avatar: ""}, urls)
	if got.AvatarURL != "https://gitbucket.example.com/avatars/alice" {
		t.Errorf("AvatarURL default = %q", got.AvatarURL)
	}
}

func TestUserTypeDefault(t *testing.T) {
	urls := NewURLBuilder("https://gitbucket.example.com")
	got := User(staticUser{login: "alice", uid: "user:alice", typ: ""}, urls)
	if got.Type != "User" {
		t.Errorf("Type default = %q, want User", got.Type)
	}
}
