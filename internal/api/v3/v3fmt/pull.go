package v3fmt

import (
	"fmt"
	"time"
)

type PullRequestSource struct {
	Owner, Repo string
	Number      int
	Title, Body string
	State       string // "open" | "closed" | "merged"
	Author      UserSource
	HeadBranch  string
	BaseBranch  string
	HeadSHA     string
	BaseSHA     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	MergedAt    *time.Time
	Draft       bool
}

type pullRefDTO struct {
	Label string  `json:"label"`
	Ref   string  `json:"ref"`
	SHA   string  `json:"sha"`
	User  UserDTO `json:"user"`
}

type PullRequestDTO struct {
	ID        int64      `json:"id"`
	NodeID    string     `json:"node_id"`
	Number    int        `json:"number"`
	State     string     `json:"state"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	URL       string     `json:"url"`
	HTMLURL   string     `json:"html_url"`
	DiffURL   string     `json:"diff_url"`
	User      UserDTO    `json:"user"`
	Head      pullRefDTO `json:"head"`
	Base      pullRefDTO `json:"base"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
	ClosedAt  string     `json:"closed_at,omitempty"`
	MergedAt  string     `json:"merged_at,omitempty"`
	Merged    bool       `json:"merged"`
	Draft     bool       `json:"draft"`
}

func PullRequest(src PullRequestSource, urls *URLBuilder) PullRequestDTO {
	id := StableID(fmt.Sprintf("pr:%s/%s:%d", src.Owner, src.Repo, src.Number))
	state := src.State
	merged := false
	if state == "merged" {
		state = "closed"
		merged = true
	}
	mergedAt := ""
	closedAt := ""
	if src.MergedAt != nil {
		mergedAt = formatTime(*src.MergedAt)
		closedAt = mergedAt
	}
	authorDTO := User(src.Author, urls)
	return PullRequestDTO{
		ID:      id,
		NodeID:  encodeNodeID("PullRequest", id),
		Number:  src.Number,
		State:   state,
		Title:   src.Title,
		Body:    src.Body,
		URL:     urls.PullAPI(src.Owner, src.Repo, src.Number),
		HTMLURL: urls.PullHTML(src.Owner, src.Repo, src.Number),
		DiffURL: urls.PullHTML(src.Owner, src.Repo, src.Number) + ".diff",
		User:    authorDTO,
		Head: pullRefDTO{
			Label: src.Owner + ":" + src.HeadBranch,
			Ref:   src.HeadBranch,
			SHA:   src.HeadSHA,
			User:  authorDTO,
		},
		Base: pullRefDTO{
			Label: src.Owner + ":" + src.BaseBranch,
			Ref:   src.BaseBranch,
			SHA:   src.BaseSHA,
			User:  authorDTO,
		},
		CreatedAt: formatTime(src.CreatedAt),
		UpdatedAt: formatTime(src.UpdatedAt),
		ClosedAt:  closedAt,
		MergedAt:  mergedAt,
		Merged:    merged,
		Draft:     src.Draft,
	}
}
