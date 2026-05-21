// Package v3fmt contains pure functions that translate internal GitBucket
// types into GitHub's exact JSON response shape. These are the only place
// that knows the GitHub REST contract; HTTP handlers in internal/api/v3 are
// thin wrappers that call existing services and delegate output to v3fmt.
package v3fmt

import (
	"fmt"
	"strings"
)

// URLBuilder constructs GitHub-shape URLs anchored at a configurable base.
type URLBuilder struct {
	base string // no trailing slash
}

func NewURLBuilder(base string) *URLBuilder {
	return &URLBuilder{base: strings.TrimRight(base, "/")}
}

func (b *URLBuilder) Base() string    { return b.base }
func (b *URLBuilder) APIRoot() string { return b.base + "/api/v3" }

func (b *URLBuilder) UserHTML(login string) string   { return b.base + "/" + login }
func (b *URLBuilder) UserAPI(login string) string    { return b.APIRoot() + "/users/" + login }
func (b *URLBuilder) UserAvatar(login string) string { return b.base + "/avatars/" + login }

func (b *URLBuilder) RepoHTML(owner, repo string) string {
	return b.base + "/" + owner + "/" + repo
}
func (b *URLBuilder) RepoAPI(owner, repo string) string {
	return b.APIRoot() + "/repos/" + owner + "/" + repo
}

func (b *URLBuilder) PullHTML(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s/%s/pulls/%d", b.base, owner, repo, number)
}
func (b *URLBuilder) PullAPI(owner, repo string, number int) string {
	return fmt.Sprintf("%s/repos/%s/%s/pulls/%d", b.APIRoot(), owner, repo, number)
}

func (b *URLBuilder) ContentsAPI(owner, repo, path, ref string) string {
	return fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", b.APIRoot(), owner, repo, path, ref)
}

func (b *URLBuilder) GitRefAPI(owner, repo, ref string) string {
	return fmt.Sprintf("%s/repos/%s/%s/git/ref/%s", b.APIRoot(), owner, repo, ref)
}

func (b *URLBuilder) GitTreeAPI(owner, repo, sha string) string {
	return fmt.Sprintf("%s/repos/%s/%s/git/trees/%s", b.APIRoot(), owner, repo, sha)
}
