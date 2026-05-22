package apps_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-chi/chi/v5"

	v3 "gitbucket/internal/api/v3"
	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestPlan3WebhookFlow(t *testing.T) {
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

	// Fake App receiver.
	var receivedMu sync.Mutex
	var receivedReq *http.Request
	var receivedBody []byte
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		receivedReq = r.Clone(context.Background())
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer appServer.Close()

	// Point the App's webhook_url at the fake server.
	_, err = fs.Collection(apps.CollectionApps).Doc(scen.App.AppID).Update(ctx,
		[]firestore.Update{{Path: "webhook_url", Value: appServer.URL}})
	if err != nil {
		t.Fatalf("update webhook_url: %v", err)
	}

	// Seed a local repo and a Plan 2 repo doc so v3 CreatePull works.
	tmp := t.TempDir()
	owner := scen.Installation.Account.ID
	repoName, err := scen.SeedRepo(ctx)
	if err != nil {
		t.Fatalf("SeedRepo: %v", err)
	}
	seedBareRepoWithFeatureBranchE2E(t, tmp, owner, repoName)

	// Wire the full router with apps + v3 + dispatcher.
	enqueuer := apps.NewMemoryEnqueuer()
	secretCache := apps.NewWebhookSecretCache(scen.Store, time.Minute)
	urls := v3fmt.NewURLBuilder("http://test.gitbucket.local")
	fireDeps := apps.FireDeps{
		FS: fs, Enqueuer: enqueuer, SecretCache: secretCache,
		URLs: urls, DispatchURL: "http://internal/_internal/dispatch-webhook",
	}

	r := chi.NewRouter()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	appsH := apps.NewHandler(fs, scen.Store, jwtV)
	apps.RegisterRoutes(r, appsH)

	v3H := v3.NewV3Handler(fs, nil, "http://test.gitbucket.local")
	v3H.LocalReposRoot = tmp
	v3H.Events = fireDeps
	v3.RegisterV3Routes(r, v3H)

	dispatcher := apps.NewDispatcherHandler(fs, "")
	r.Post("/_internal/dispatch-webhook/{id}", dispatcher.Dispatch)

	// Mint installation token.
	jwtStr := scen.SignJWT(t)
	mintReq := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	mintReq.Header.Set("Authorization", "Bearer "+jwtStr)
	mintRR := httptest.NewRecorder()
	r.ServeHTTP(mintRR, mintReq)
	if mintRR.Code != http.StatusCreated {
		t.Fatalf("mint failed: %d %s", mintRR.Code, mintRR.Body.String())
	}
	var minted map[string]interface{}
	_ = json.Unmarshal(mintRR.Body.Bytes(), &minted)
	tok := minted["token"].(string)

	// Create a PR via v3 → should enqueue a pull_request webhook task.
	prReq := httptest.NewRequest("POST",
		"/api/v3/repos/"+owner+"/"+repoName+"/pulls",
		bytes.NewBufferString(`{"title":"E2E webhook PR","head":"feature","base":"main"}`))
	prReq.Header.Set("Authorization", "Bearer "+tok)
	prReq.Header.Set("Content-Type", "application/json")
	prRR := httptest.NewRecorder()
	r.ServeHTTP(prRR, prReq)
	if prRR.Code != http.StatusCreated {
		t.Fatalf("create pull failed: %d %s", prRR.Code, prRR.Body.String())
	}

	// Drain the enqueuer.
	tasks := enqueuer.Drain()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(tasks))
	}
	task := tasks[0]

	// Replay the task through the dispatcher (simulates Cloud Tasks).
	dispReq := httptest.NewRequest("POST", task.TargetURL, bytes.NewReader(task.Body))
	for k, v := range task.Headers {
		dispReq.Header.Set(k, v)
	}
	dispRR := httptest.NewRecorder()
	r.ServeHTTP(dispRR, dispReq)
	if dispRR.Code != http.StatusOK {
		t.Fatalf("dispatcher returned %d: %s", dispRR.Code, dispRR.Body.String())
	}

	// Assert the fake App received the relay with valid signature.
	receivedMu.Lock()
	defer receivedMu.Unlock()
	if receivedReq == nil {
		t.Fatal("fake App did not receive a request")
	}
	if receivedReq.Header.Get("X-GitHub-Event") != "pull_request" {
		t.Errorf("X-GitHub-Event = %q", receivedReq.Header.Get("X-GitHub-Event"))
	}
	if !strings.HasPrefix(receivedReq.Header.Get("X-Hub-Signature-256"), "sha256=") {
		t.Error("X-Hub-Signature-256 missing or malformed")
	}

	// Verify the signature on the received body.
	secret, _ := secretCache.Get(ctx, scen.App.WebhookSecretResource)
	if !apps.VerifyHubSignature(receivedBody, secret, receivedReq.Header.Get("X-Hub-Signature-256")) {
		t.Error("X-Hub-Signature-256 does not validate against the webhook secret")
	}

	// Sanity-check the body shape.
	var body map[string]interface{}
	if err := json.Unmarshal(receivedBody, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["action"] != "opened" {
		t.Errorf("action = %v", body["action"])
	}
}

// seedBareRepoWithFeatureBranchE2E creates a bare repo with main + a feature
// branch, both with at least one commit. Self-contained (does not depend on
// helpers in other _test.go files).
func seedBareRepoWithFeatureBranchE2E(t *testing.T, root, owner, repo string) {
	t.Helper()
	bare := filepath.Join(root, owner+"_"+repo+".git")
	work := filepath.Join(root, "_e2e_work_"+owner+"_"+repo)
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	run := func(dir, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run(bare, "git", "init", "--bare", "--initial-branch=main")
	run(work, "git", "init", "--initial-branch=main")
	run(work, "git", "config", "user.email", "test@test")
	run(work, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# Hello\n"), 0o644)
	os.MkdirAll(filepath.Join(work, "src"), 0o755)
	os.WriteFile(filepath.Join(work, "src", "main.go"), []byte("package main\n"), 0o644)
	run(work, "git", "add", ".")
	run(work, "git", "commit", "-m", "init")
	run(work, "git", "remote", "add", "origin", bare)
	run(work, "git", "push", "origin", "main")

	// Create a feature branch with an additional commit.
	run(work, "git", "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o644)
	run(work, "git", "add", ".")
	run(work, "git", "commit", "-m", "feature")
	run(work, "git", "push", "origin", "feature")
}
