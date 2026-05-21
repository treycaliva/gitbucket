package apps_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestFire_EnqueuesForMatchingInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)
	owner := scen.Installation.Account.ID

	enq := apps.NewMemoryEnqueuer()
	cache := apps.NewWebhookSecretCache(scen.Store, time.Minute)
	deps := apps.FireDeps{
		FS:          fs,
		Enqueuer:    enq,
		SecretCache: cache,
		URLs:        v3fmt.NewURLBuilder("https://gb.test"),
		DispatchURL: "https://gb.test/_internal/dispatch-webhook",
	}

	apps.Fire(ctx, deps, apps.PullRequestPayload{
		Action: "opened", Number: 1, Title: "x", State: "open",
		HeadBranch: "f", BaseBranch: "main",
		OwnerLogin: owner, RepoName: "any-repo",
		Sender: apps.SenderRef{Login: "alice", ID: 1, Type: "User"},
	})

	tasks := enq.Drain()
	if len(tasks) != 1 {
		t.Fatalf("Drain len = %d, want 1", len(tasks))
	}
	tt := tasks[0]
	if tt.Headers["X-GitHub-Event"] != "pull_request" {
		t.Errorf("X-GitHub-Event = %q", tt.Headers["X-GitHub-Event"])
	}
	if tt.Headers["X-Hub-Signature-256"] == "" {
		t.Error("X-Hub-Signature-256 missing")
	}
	if !strings.HasPrefix(tt.TargetURL, "https://gb.test/_internal/dispatch-webhook/") {
		t.Errorf("TargetURL = %q", tt.TargetURL)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(tt.Body, &body); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	for _, key := range []string{"action", "number", "pull_request", "repository", "installation", "sender"} {
		if _, ok := body[key]; !ok {
			t.Errorf("payload missing key %q", key)
		}
	}
}

func TestFire_SkipsBotSender(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := testfixtures.NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	enq := apps.NewMemoryEnqueuer()
	deps := apps.FireDeps{
		FS:          fs,
		Enqueuer:    enq,
		SecretCache: apps.NewWebhookSecretCache(scen.Store, time.Minute),
		URLs:        v3fmt.NewURLBuilder("https://gb.test"),
		DispatchURL: "https://gb.test/_internal/dispatch-webhook",
	}

	apps.Fire(ctx, deps, apps.PullRequestPayload{
		Action: "opened", Number: 1, State: "open",
		OwnerLogin: scen.Installation.Account.ID, RepoName: "r",
		Sender: apps.SenderRef{Login: "gitbucket-sync-bot", ID: 999, Type: "Bot"},
	})

	if got := enq.Drain(); len(got) != 0 {
		t.Errorf("expected 0 tasks for bot sender, got %d", len(got))
	}
}
