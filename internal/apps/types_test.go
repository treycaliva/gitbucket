package apps

import (
	"testing"
	"time"
)

func TestPermissionLevelString(t *testing.T) {
	cases := []struct {
		level PermissionLevel
		want  string
	}{
		{PermNone, "none"},
		{PermRead, "read"},
		{PermWrite, "write"},
	}
	for _, c := range cases {
		if got := c.level.String(); got != c.want {
			t.Errorf("PermissionLevel(%d).String() = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestPermissionsSatisfies(t *testing.T) {
	have := Permissions{"issues": PermWrite, "contents": PermRead}
	if !have.Satisfies("issues", PermWrite) {
		t.Error("write should satisfy write")
	}
	if !have.Satisfies("issues", PermRead) {
		t.Error("write should satisfy read")
	}
	if have.Satisfies("contents", PermWrite) {
		t.Error("read should not satisfy write")
	}
	if have.Satisfies("metadata", PermRead) {
		t.Error("missing permission should not satisfy")
	}
}

func TestAccountRefRoundTrip(t *testing.T) {
	a := AccountRef{ID: "uid-123", Type: AccountTypeUser}
	if a.Type != "User" {
		t.Errorf("AccountTypeUser = %q, want %q", a.Type, "User")
	}
}

func TestInstallationTokenExpired(t *testing.T) {
	tok := &InstallationToken{ExpiresAt: time.Now().Add(-time.Second)}
	if !tok.Expired(time.Now()) {
		t.Error("expected token to be expired")
	}
	tok.ExpiresAt = time.Now().Add(time.Hour)
	if tok.Expired(time.Now()) {
		t.Error("expected token not to be expired")
	}
}
