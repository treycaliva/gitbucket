package apps

import (
	"context"
	"testing"
)

func TestMemoryEnqueuer_Enqueue(t *testing.T) {
	ctx := context.Background()
	e := NewMemoryEnqueuer()
	if err := e.Enqueue(ctx, TaskSpec{
		TargetURL: "https://internal/dispatch/abc",
		Headers:   map[string]string{"X-GitHub-Event": "pull_request"},
		Body:      []byte(`{"action":"opened"}`),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	tasks := e.Drain()
	if len(tasks) != 1 {
		t.Fatalf("Drain len = %d, want 1", len(tasks))
	}
	if tasks[0].TargetURL != "https://internal/dispatch/abc" {
		t.Errorf("TargetURL = %q", tasks[0].TargetURL)
	}
	if tasks[0].Headers["X-GitHub-Event"] != "pull_request" {
		t.Errorf("header missing")
	}
	if string(tasks[0].Body) != `{"action":"opened"}` {
		t.Errorf("body = %q", tasks[0].Body)
	}
}

func TestMemoryEnqueuer_DrainResets(t *testing.T) {
	e := NewMemoryEnqueuer()
	_ = e.Enqueue(context.Background(), TaskSpec{TargetURL: "x"})
	if got := e.Drain(); len(got) != 1 {
		t.Fatalf("first Drain len = %d", len(got))
	}
	if got := e.Drain(); len(got) != 0 {
		t.Errorf("second Drain len = %d, want 0 (Drain should reset)", len(got))
	}
}
