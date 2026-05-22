package apps

import (
	"context"
	"fmt"
	"sync"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
)

// TaskSpec is the input to TaskEnqueuer.Enqueue. The body and headers are
// dispatched verbatim to TargetURL; the enqueuer adds an OIDC token in the
// real implementation (memory implementation does not).
type TaskSpec struct {
	TargetURL string
	Headers   map[string]string
	Body      []byte
}

// TaskEnqueuer abstracts the task queue so tests can substitute an
// in-memory recorder. Implementations must be safe for concurrent use.
type TaskEnqueuer interface {
	Enqueue(ctx context.Context, spec TaskSpec) error
}

// --- Memory implementation (tests) -----------------------------------------

type MemoryEnqueuer struct {
	mu    sync.Mutex
	tasks []TaskSpec
}

func NewMemoryEnqueuer() *MemoryEnqueuer { return &MemoryEnqueuer{} }

func (m *MemoryEnqueuer) Enqueue(_ context.Context, spec TaskSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Defensive copy of header map + body so caller mutations don't leak.
	hCopy := make(map[string]string, len(spec.Headers))
	for k, v := range spec.Headers {
		hCopy[k] = v
	}
	bCopy := make([]byte, len(spec.Body))
	copy(bCopy, spec.Body)
	m.tasks = append(m.tasks, TaskSpec{TargetURL: spec.TargetURL, Headers: hCopy, Body: bCopy})
	return nil
}

// Drain returns all enqueued tasks and clears the buffer. Tests use this to
// assert what got enqueued during a single test scenario.
func (m *MemoryEnqueuer) Drain() []TaskSpec {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.tasks
	m.tasks = nil
	return out
}

// --- Cloud Tasks implementation (production) -------------------------------

// RealEnqueuer wraps a Cloud Tasks client and a fully-qualified queue path.
// Caller supplies queueName like "projects/<p>/locations/<l>/queues/<q>"
// and oidcServiceAccount + audience that the dispatcher endpoint expects.
type RealEnqueuer struct {
	client             *cloudtasks.Client
	queueName          string
	oidcServiceAccount string
	oidcAudience       string
}

func NewRealEnqueuer(client *cloudtasks.Client, queueName, oidcServiceAccount, oidcAudience string) *RealEnqueuer {
	return &RealEnqueuer{
		client:             client,
		queueName:          queueName,
		oidcServiceAccount: oidcServiceAccount,
		oidcAudience:       oidcAudience,
	}
}

func (e *RealEnqueuer) Enqueue(ctx context.Context, spec TaskSpec) error {
	req := &taskspb.CreateTaskRequest{
		Parent: e.queueName,
		Task: &taskspb.Task{
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					Url:        spec.TargetURL,
					HttpMethod: taskspb.HttpMethod_POST,
					Headers:    spec.Headers,
					Body:       spec.Body,
					AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
						OidcToken: &taskspb.OidcToken{
							ServiceAccountEmail: e.oidcServiceAccount,
							Audience:            e.oidcAudience,
						},
					},
				},
			},
		},
	}
	if _, err := e.client.CreateTask(ctx, req); err != nil {
		return fmt.Errorf("cloud tasks enqueue: %w", err)
	}
	return nil
}
