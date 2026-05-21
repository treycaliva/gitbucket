package v3fmt

import (
	"encoding/base64"
	"fmt"
)

// UserSource is a minimal interface the User formatter requires.
type UserSource interface {
	GetLogin() string
	GetUserKey() string // stable string used to derive int64 id
	GetType() string    // "User" or "Bot"
	GetAvatarURL() string
}

// UserDTO matches GitHub's `user` JSON shape (subset).
type UserDTO struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	NodeID    string `json:"node_id"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
	URL       string `json:"url"`
	Type      string `json:"type"`
	SiteAdmin bool   `json:"site_admin"`
}

func User(src UserSource, urls *URLBuilder) UserDTO {
	id := StableID(src.GetUserKey())
	avatar := src.GetAvatarURL()
	if avatar == "" {
		avatar = urls.UserAvatar(src.GetLogin())
	}
	typ := src.GetType()
	if typ == "" {
		typ = "User"
	}
	return UserDTO{
		Login:     src.GetLogin(),
		ID:        id,
		NodeID:    encodeNodeID("User", id),
		AvatarURL: avatar,
		HTMLURL:   urls.UserHTML(src.GetLogin()),
		URL:       urls.UserAPI(src.GetLogin()),
		Type:      typ,
		SiteAdmin: false,
	}
}

// UserFromMap converts a Firestore raw users-doc map to a GitHub-shape user.
func UserFromMap(m map[string]interface{}, urls *URLBuilder) UserDTO {
	login, _ := m["username"].(string)
	uid, _ := m["uid"].(string)
	typ, _ := m["type"].(string)
	avatar, _ := m["avatar_url"].(string)
	return User(StaticUser(login, "user:"+uid, typ, avatar), urls)
}

// UserFromBot synthesizes the App's bot user from primitive values (no
// dependency on internal/apps to keep this package free of project cycles).
func UserFromBot(appID, botUserID string, urls *URLBuilder) UserDTO {
	return User(StaticUser(
		"app-"+appID+"[bot]",
		"user:"+botUserID,
		"Bot",
		"",
	), urls)
}

// StaticUser constructs an in-memory UserSource. Exported so other formatters
// can synthesize user sources from primitive values.
func StaticUser(login, uid, typ, avatarURL string) UserSource {
	return staticUserSource{login: login, uid: uid, typ: typ, avatar: avatarURL}
}

type staticUserSource struct {
	login, uid, typ, avatar string
}

func (s staticUserSource) GetLogin() string     { return s.login }
func (s staticUserSource) GetUserKey() string   { return s.uid }
func (s staticUserSource) GetType() string      { return s.typ }
func (s staticUserSource) GetAvatarURL() string { return s.avatar }

func encodeNodeID(kind string, id int64) string {
	raw := fmt.Sprintf("%s:%d", kind, id)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}
