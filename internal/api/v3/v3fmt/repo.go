package v3fmt

import "time"

// RepoSource carries the minimum fields needed to render a GitHub-shape
// repository.
type RepoSource struct {
	Owner         UserSource
	Name          string
	Description   string
	Visibility    string // "public" | "private"
	DefaultBranch string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RepositoryDTO matches GitHub's repository JSON shape (subset that GitBucket
// can populate — omits forks/watchers/etc).
type RepositoryDTO struct {
	ID            int64   `json:"id"`
	NodeID        string  `json:"node_id"`
	Name          string  `json:"name"`
	FullName      string  `json:"full_name"`
	Owner         UserDTO `json:"owner"`
	Private       bool    `json:"private"`
	HTMLURL       string  `json:"html_url"`
	Description   string  `json:"description"`
	Fork          bool    `json:"fork"`
	URL           string  `json:"url"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	PushedAt      string  `json:"pushed_at"`
	DefaultBranch string  `json:"default_branch"`
	Visibility    string  `json:"visibility"`
}

func Repository(src RepoSource, urls *URLBuilder) RepositoryDTO {
	id := StableID("repo:" + src.Owner.GetLogin() + "/" + src.Name)
	return RepositoryDTO{
		ID:            id,
		NodeID:        encodeNodeID("Repository", id),
		Name:          src.Name,
		FullName:      src.Owner.GetLogin() + "/" + src.Name,
		Owner:         User(src.Owner, urls),
		Private:       src.Visibility == "private",
		HTMLURL:       urls.RepoHTML(src.Owner.GetLogin(), src.Name),
		Description:   src.Description,
		Fork:          false,
		URL:           urls.RepoAPI(src.Owner.GetLogin(), src.Name),
		CreatedAt:     formatTime(src.CreatedAt),
		UpdatedAt:     formatTime(src.UpdatedAt),
		PushedAt:      formatTime(src.UpdatedAt), // best approximation
		DefaultBranch: src.DefaultBranch,
		Visibility:    src.Visibility,
	}
}

// RepositoryFromMap converts a Firestore raw repo doc (as returned by
// db.GetRepositoryMetadata) to a GitHub-shape DTO.
func RepositoryFromMap(m map[string]interface{}, urls *URLBuilder) RepositoryDTO {
	ownerLogin, _ := m["owner"].(string)
	ownerUID, _ := m["ownerUid"].(string)
	owner := StaticUser(ownerLogin, "user:"+ownerUID, "User", "")
	return Repository(RepoSource{
		Owner:         owner,
		Name:          getString(m, "name"),
		Description:   getString(m, "description"),
		Visibility:    getString(m, "visibility"),
		DefaultBranch: getString(m, "defaultBranch"),
		CreatedAt:     getTime(m, "createdAt"),
		UpdatedAt:     getTime(m, "updatedAt"),
	}, urls)
}

// --- shared helpers ----------------------------------------------------

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func getTime(m map[string]interface{}, key string) time.Time {
	v, _ := m[key].(time.Time)
	return v
}
