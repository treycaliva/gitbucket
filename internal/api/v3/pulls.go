package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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

func (h *V3Handler) CreatePull(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermWrite); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Draft bool   `json:"draft"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}
	if req.Title == "" || req.Head == "" || req.Base == "" {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}

	ic := apps.InstallationContextFrom(r.Context())
	if ic == nil {
		apps.WriteError(w, apps.ErrUnauthorized)
		return
	}

	botLogin := "app-" + ic.AppID + "[bot]"
	pr, err := db.CreatePullRequest(r.Context(), h.FirestoreClient, owner, repo,
		req.Title, req.Body, req.Head, req.Base, ic.BotUserID, botLogin)
	if err != nil {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}
	apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
		Action:     "opened",
		Number:     pr.Number,
		Title:      pr.Title,
		Body:       pr.Description,
		State:      "open",
		HeadBranch: pr.SourceBranch,
		BaseBranch: pr.TargetBranch,
		OwnerLogin: owner,
		RepoName:   repo,
		Sender:     senderFromCtx(r.Context()),
	})
	apps.WriteJSON(w, http.StatusCreated, pullToFormatter(*pr, owner, repo, h.URLs))
}

func (h *V3Handler) UpdatePull(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "pull_requests", apps.PermWrite); err != nil {
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

	var req struct {
		Title string  `json:"title"`
		Body  *string `json:"body"` // pointer so we can distinguish "omitted" from "empty string"
		State string  `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apps.WriteError(w, apps.ErrUnprocessable)
		return
	}

	body := ""
	if req.Body != nil {
		body = *req.Body
	}
	if req.Title != "" || body != "" {
		if err := db.UpdatePullRequestTitleBody(r.Context(), h.FirestoreClient, owner, repo, num, req.Title, body); err != nil {
			apps.WriteError(w, err)
			return
		}
	}
	if req.State == "closed" || req.State == "open" {
		if err := db.UpdatePullRequestStatus(r.Context(), h.FirestoreClient, owner, repo, num, req.State); err != nil {
			apps.WriteError(w, err)
			return
		}
	}

	pr, err := db.GetPullRequest(r.Context(), h.FirestoreClient, owner, repo, num)
	if err != nil || pr == nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	action := "edited"
	if req.State == "closed" {
		action = "closed"
	} else if req.State == "open" {
		action = "reopened"
	}
	apps.Fire(r.Context(), h.Events, apps.PullRequestPayload{
		Action:     action,
		Number:     pr.Number,
		Title:      pr.Title,
		Body:       pr.Description,
		State:      pr.Status, // "open" | "closed" | "merged"
		HeadBranch: pr.SourceBranch,
		BaseBranch: pr.TargetBranch,
		OwnerLogin: owner,
		RepoName:   repo,
		Sender:     senderFromCtx(r.Context()),
	})
	apps.WriteJSON(w, http.StatusOK, pullToFormatter(*pr, owner, repo, h.URLs))
}

// senderFromCtx derives a SenderRef from the installation context. The
// actor on any v3 write is the App's bot user.
func senderFromCtx(ctx context.Context) apps.SenderRef {
	ic := apps.InstallationContextFrom(ctx)
	if ic == nil {
		return apps.SenderRef{Login: "unknown", Type: "User"}
	}
	return apps.SenderRef{
		Login: "app-" + ic.AppID + "[bot]",
		Type:  "Bot",
	}
}

// pullToFormatter translates db.PullRequest → v3fmt.PullRequestDTO.
// IMPORTANT: db.PullRequest has no UpdatedAt/MergedAt/ClosedAt fields, so we
// approximate UpdatedAt as CreatedAt and pass nil MergedAt. The Status field
// values are "open" | "merged" | "closed"; formatter maps "merged" → state=closed + merged=true.
func pullToFormatter(pr db.PullRequest, owner, repo string, urls *v3fmt.URLBuilder) v3fmt.PullRequestDTO {
	userType := "User"
	if strings.HasSuffix(pr.AuthorUsername, "[bot]") {
		userType = "Bot"
	}
	author := v3fmt.StaticUser(pr.AuthorUsername, "user:"+pr.AuthorUID, userType, "")
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
