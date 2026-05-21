package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"gitbucket/internal/api/v3/v3fmt"
)

// EventType enumerates webhook event names. Values match GitHub's
// X-GitHub-Event header.
type EventType int

const (
	EventPullRequest EventType = iota + 1
	EventPush
	EventInstallation
)

func (t EventType) String() string {
	switch t {
	case EventPullRequest:
		return "pull_request"
	case EventPush:
		return "push"
	case EventInstallation:
		return "installation"
	default:
		return "unknown"
	}
}

// SenderRef carries the minimum identity needed to populate the `sender`
// block of any GitHub-shape webhook payload. Sourced from the actor whose
// action triggered the event.
type SenderRef struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"` // "User" or "Bot"
}

// Payload is the interface that all event payloads implement.
type Payload interface {
	Event() EventType
	// Owner is the repo owner login. Empty for non-repo-scoped events.
	Owner() string
	// SenderRef returns the actor whose action triggered the event.
	SenderRef() SenderRef
}

// PullRequestPayload is the input to a `pull_request` event Fire call.
type PullRequestPayload struct {
	Action     string // "opened" | "edited" | "closed" | "reopened"
	Number     int
	Title      string
	Body       string
	State      string // "open" | "closed"
	HeadBranch string
	BaseBranch string
	HeadSHA    string
	BaseSHA    string
	Merged     bool
	MergedAt   *time.Time

	OwnerLogin string
	RepoName   string
	Sender     SenderRef
}

func (p PullRequestPayload) Event() EventType    { return EventPullRequest }
func (p PullRequestPayload) Owner() string        { return p.OwnerLogin }
func (p PullRequestPayload) SenderRef() SenderRef { return p.Sender }

// PushPayload represents a `push` event.
type PushPayload struct {
	Ref     string
	Before  string
	After   string
	Commits []string

	OwnerLogin string
	RepoName   string
	Sender     SenderRef
}

func (p PushPayload) Event() EventType    { return EventPush }
func (p PushPayload) Owner() string        { return p.OwnerLogin }
func (p PushPayload) SenderRef() SenderRef { return p.Sender }

// InstallationPayload represents an `installation` event. Fire wiring for
// this event is added in Plan 4 alongside the install flow.
type InstallationPayload struct {
	Action  string // "created" | "deleted"
	AppID   string
	Account AccountRef
	Sender  SenderRef
}

func (p InstallationPayload) Event() EventType    { return EventInstallation }
func (p InstallationPayload) Owner() string        { return p.Account.ID }
func (p InstallationPayload) SenderRef() SenderRef { return p.Sender }

// FireDeps bundles the runtime dependencies needed by Fire. Construct once
// in main() and store on something accessible from event-firing handlers
// (e.g. on V3Handler / APIHandler).
type FireDeps struct {
	FS          *firestore.Client
	Enqueuer    TaskEnqueuer
	SecretCache *WebhookSecretCache
	URLs        *v3fmt.URLBuilder
	DispatchURL string // base URL for the dispatcher endpoint
}

// Fire enqueues webhook deliveries for every installation that has
// subscribed to this event on the relevant repository. Non-blocking.
// Failures are logged but never returned — the user-visible request that
// triggered the event must not fail because of webhook trouble.
func Fire(ctx context.Context, deps FireDeps, p Payload) {
	if deps.FS == nil || deps.Enqueuer == nil || deps.SecretCache == nil {
		FireError("Fire called with incomplete deps; webhook delivery skipped")
		return
	}
	// 1. Loop prevention: drop events whose sender is a known bot identity.
	if IsBotIdentity(ctx, p.SenderRef().Login) {
		return
	}
	// 2. Find matching installations.
	insts, err := matchingInstallations(ctx, deps.FS, p)
	if err != nil {
		FireError("matchingInstallations: %v", err)
		return
	}
	// 3. Fan out.
	for _, inst := range insts {
		fireOne(ctx, deps, inst, p)
	}
}

func fireOne(ctx context.Context, deps FireDeps, inst *Installation, p Payload) {
	payloadBytes, err := buildEventBody(deps, inst, p)
	if err != nil {
		FireError("buildEventBody: %v", err)
		return
	}

	app, err := GetApp(ctx, deps.FS, inst.AppID)
	if err != nil || app == nil {
		FireError("GetApp(%s): %v", inst.AppID, err)
		return
	}

	secret, err := deps.SecretCache.Get(ctx, app.WebhookSecretResource)
	if err != nil {
		FireError("secret fetch for app %s: %v", app.AppID, err)
		return
	}

	signature := ComputeHubSignature(payloadBytes, secret)
	payloadHash := sha256.Sum256(payloadBytes)

	deliv, err := CreateDelivery(ctx, deps.FS, CreateDeliveryInput{
		AppID:          app.AppID,
		InstallationID: inst.InstallationID,
		Event:          p.Event().String(),
		TargetURL:      app.WebhookURL,
		PayloadSHA256:  hex.EncodeToString(payloadHash[:]),
	})
	if err != nil {
		FireError("CreateDelivery: %v", err)
		return
	}

	headers := map[string]string{
		"Content-Type":                           "application/json",
		"User-Agent":                             "GitBucket-Hookshot/1.0",
		"X-GitHub-Event":                         p.Event().String(),
		"X-GitHub-Delivery":                      deliv.DeliveryID,
		"X-Hub-Signature-256":                    signature,
		"X-GitHub-Hook-ID":                       app.AppID,
		"X-GitHub-Hook-Installation-Target-Type": "repository",
		"X-GitHub-Hook-Installation-Target-ID":   inst.InstallationID,
	}

	target := deps.DispatchURL + "/" + deliv.DeliveryID
	if err := deps.Enqueuer.Enqueue(ctx, TaskSpec{
		TargetURL: target,
		Headers:   headers,
		Body:      payloadBytes,
	}); err != nil {
		FireError("enqueue delivery %s: %v", deliv.DeliveryID, err)
		_ = UpdateDeliveryStatus(ctx, deps.FS, deliv.DeliveryID, DeliveryUpdate{
			Status: "failed", Attempts: 0,
		})
	}
}

// matchingInstallations finds installations subscribing to the event on the
// owner's repos.
func matchingInstallations(ctx context.Context, fs *firestore.Client, p Payload) ([]*Installation, error) {
	owner := p.Owner()
	if owner == "" {
		return nil, nil
	}
	all, err := listInstallationsByAccount(ctx, fs, owner)
	if err != nil {
		return nil, err
	}
	out := make([]*Installation, 0, len(all))
	for _, inst := range all {
		if inst.SuspendedAt != nil {
			continue
		}
		if !subscribesToEvent(inst.Events, p.Event()) {
			continue
		}
		if inst.RepositorySelection == "selected" {
			repoID := repoIDFor(p)
			if !containsString(inst.RepositoryIDs, repoID) {
				continue
			}
		}
		out = append(out, inst)
	}
	return out, nil
}

func listInstallationsByAccount(ctx context.Context, fs *firestore.Client, accountID string) ([]*Installation, error) {
	var out []*Installation
	iter := fs.Collection(CollectionInstallations).Where("account.id", "==", accountID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var inst Installation
		if err := doc.DataTo(&inst); err != nil {
			return nil, err
		}
		out = append(out, &inst)
	}
	return out, nil
}

func subscribesToEvent(events []string, t EventType) bool {
	name := t.String()
	for _, e := range events {
		if e == name {
			return true
		}
	}
	return false
}

// repoIDFor returns the Firestore repo doc-ID for the payload, matching
// db.CreateRepositoryMetadata's "<lower-owner>_<lower-name>" scheme.
// NOTE: uses OwnerLogin/RepoName (renamed in Task 1 to avoid field/method collision).
func repoIDFor(p Payload) string {
	switch v := p.(type) {
	case PullRequestPayload:
		return strings.ToLower(v.OwnerLogin) + "_" + strings.ToLower(v.RepoName)
	case PushPayload:
		return strings.ToLower(v.OwnerLogin) + "_" + strings.ToLower(v.RepoName)
	default:
		return ""
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// buildEventBody marshals the GitHub-shape JSON payload for the event.
func buildEventBody(deps FireDeps, inst *Installation, p Payload) ([]byte, error) {
	envelope := map[string]interface{}{
		"installation": map[string]interface{}{
			"id":      inst.InstallationID,
			"node_id": "I_" + inst.InstallationID,
		},
		"sender": map[string]interface{}{
			"login": p.SenderRef().Login,
			"id":    p.SenderRef().ID,
			"type":  p.SenderRef().Type,
		},
	}

	switch v := p.(type) {
	case PullRequestPayload:
		envelope["action"] = v.Action
		envelope["number"] = v.Number
		envelope["pull_request"] = v3fmt.PullRequest(v3fmt.PullRequestSource{
			Owner:      v.OwnerLogin,
			Repo:       v.RepoName,
			Number:     v.Number,
			Title:      v.Title,
			Body:       v.Body,
			State:      v.State,
			Author:     v3fmt.StaticUser(p.SenderRef().Login, "user:"+p.SenderRef().Login, p.SenderRef().Type, ""),
			HeadBranch: v.HeadBranch,
			BaseBranch: v.BaseBranch,
			HeadSHA:    v.HeadSHA,
			BaseSHA:    v.BaseSHA,
			MergedAt:   v.MergedAt,
		}, deps.URLs)
		envelope["repository"] = repoEnvelope(deps, v.OwnerLogin, v.RepoName)
	case PushPayload:
		envelope["ref"] = v.Ref
		envelope["before"] = v.Before
		envelope["after"] = v.After
		envelope["commits"] = v.Commits
		envelope["repository"] = repoEnvelope(deps, v.OwnerLogin, v.RepoName)
	case InstallationPayload:
		envelope["action"] = v.Action
	}

	return json.Marshal(envelope)
}

func repoEnvelope(deps FireDeps, owner, repo string) map[string]interface{} {
	return map[string]interface{}{
		"full_name": owner + "/" + repo,
		"name":      repo,
		"owner":     map[string]interface{}{"login": owner},
		"url":       deps.URLs.RepoAPI(owner, repo),
		"html_url":  deps.URLs.RepoHTML(owner, repo),
	}
}

// FireError logs a non-fatal error from the Fire path. Webhook trouble must
// never fail the originating user request.
func FireError(format string, args ...interface{}) {
	logEvents("ERROR: " + fmt.Sprintf(format, args...))
}

// logEvents is the single log sink for the events package.
var logEvents = func(s string) {
	fmt.Println("[apps.events] " + s)
}
