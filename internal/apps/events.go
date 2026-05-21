package apps

import (
	"context"
	"fmt"
	"time"
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

// Fire enqueues webhook deliveries for every installation that has
// subscribed to this event on the relevant repository. Non-blocking. The
// full implementation lands in Task 7; this is a no-op stub that lets
// caller code be wired in advance.
func Fire(ctx context.Context, p Payload) {
	_ = ctx
	_ = p
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
