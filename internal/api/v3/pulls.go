package v3

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

func (h *V3Handler) ListPulls(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	prs, err := db.ListPullRequests(r.Context(), h.FirestoreClient, owner, repo)
	if err != nil {
		apps.WriteError(w, err)
		return
	}
	out := make([]v3fmt.PullRequestDTO, 0, len(prs))
	for _, pr := range prs {
		out = append(out, pullToFormatter(*pr, owner, repo, h.URLs))
	}
	apps.WriteJSON(w, http.StatusOK, out)
}

func (h *V3Handler) GetPull(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	numStr := chi.URLParam(r, "number")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, num)
	if err != nil {
		apps.WriteError(w, err)
		return
	}
	if pr == nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, pullToFormatter(*pr, owner, repo, h.URLs))
}

// pullToFormatter translates db.PullRequest → v3fmt.PullRequestDTO.
// IMPORTANT: db.PullRequest has no UpdatedAt/MergedAt/ClosedAt fields, so we
// approximate UpdatedAt as CreatedAt and pass nil MergedAt. The Status field
// values are "open" | "merged" | "closed"; formatter maps "merged" → state=closed + merged=true.
func pullToFormatter(pr db.PullRequest, owner, repo string, urls *v3fmt.URLBuilder) v3fmt.PullRequestDTO {
	author := v3fmt.StaticUser(pr.AuthorUsername, "user:"+pr.AuthorUID, "User", "")
	return v3fmt.PullRequest(v3fmt.PullRequestSource{
		Owner:      owner,
		Repo:       repo,
		Number:     pr.Number,
		Title:      pr.Title,
		Body:       pr.Description,
		State:      pr.Status,
		Author:     author,
		HeadBranch: pr.SourceBranch,
		BaseBranch: pr.TargetBranch,
		CreatedAt:  pr.CreatedAt,
		UpdatedAt:  pr.CreatedAt, // db has no separate updated_at
	}, urls)
}
