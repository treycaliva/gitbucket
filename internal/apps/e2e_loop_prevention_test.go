package apps_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestPlan3LoopPrevention_AppInitiatedEvents verifies that when an App creates
// a PR via v3, the resulting pull_request event is NOT enqueued back to that
// same App (loop prevention via IsBotIdentity on the sender "<slug>[bot]").
func TestPlan3LoopPrevention_AppInitiatedEvents(t *testing.T) {
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

	// Point webhook_url at a no-op sink — it should never be called.
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()
	_, err = fs.Collection(apps.CollectionApps).Doc(scen.App.AppID).Update(ctx,
		[]firestore.Update{{Path: "webhook_url", Value: sink.URL}})
	if err != nil {
		t.Fatalf("update webhook_url: %v", err)
	}

	// Seed a local bare repo with main + feature branch so CreatePull succeeds.
	tmp := t.TempDir()
	owner := scen.Installation.Account.ID
	repoName, err := scen.SeedRepo(ctx)
	if err != nil {
		t.Fatalf("SeedRepo: %v", err)
	}
	seedBareRepoWithFeatureBranchLoop(t, tmp, owner, repoName)

	// Refresh DefaultBotIdentityCache BEFORE the PR create so the bot user
	// seeded by NewScenario is recognised by IsBotIdentity during Fire.
	botCache := apps.NewBotIdentityCache(fs, 60*time.Second)
	apps.SetDefaultBotIdentityCache(botCache)
	if err := botCache.Refresh(ctx); err != nil {
		t.Fatalf("bot cache refresh: %v", err)
	}

	// Wire up the router.
	enqueuer := apps.NewMemoryEnqueuer()
	secretCache := apps.NewWebhookSecretCache(scen.Store, time.Minute)
	urls := v3fmt.NewURLBuilder("http://t")
	fireDeps := apps.FireDeps{
		FS: fs, Enqueuer: enqueuer, SecretCache: secretCache,
		URLs:        urls,
		DispatchURL: "http://t/_internal/dispatch-webhook",
	}

	r := chi.NewRouter()
	jwtV := apps.NewJWTVerifier(fs, 60*time.Second)
	appsH := apps.NewHandler(fs, scen.Store, jwtV)
	apps.RegisterRoutes(r, appsH)

	v3H := v3.NewV3Handler(fs, nil, "http://t")
	v3H.LocalReposRoot = tmp
	v3H.Events = fireDeps
	v3.RegisterV3Routes(r, v3H)

	// Mint installation token.
	jwtStr := scen.SignJWT(t)
	mintReq := httptest.NewRequest("POST",
		"/api/v3/app/installations/"+scen.Installation.InstallationID+"/access_tokens",
		bytes.NewBufferString(`{}`))
	mintReq.Header.Set("Authorization", "Bearer "+jwtStr)
	mintRR := httptest.NewRecorder()
	r.ServeHTTP(mintRR, mintReq)
	if mintRR.Code != http.StatusCreated {
		t.Fatalf("mint: %d %s", mintRR.Code, mintRR.Body.String())
	}
	var minted map[string]interface{}
	if err := json.Unmarshal(mintRR.Body.Bytes(), &minted); err != nil {
		t.Fatalf("unmarshal mint response: %v", err)
	}
	tok, _ := minted["token"].(string)
	if tok == "" {
		t.Fatalf("no token in mint response: %v", minted)
	}

	// App-initiated PR via v3. The sender will be "<slug>[bot]" which the
	// bot identity cache now knows — so Fire should skip all installations
	// and enqueue nothing.
	prReq := httptest.NewRequest("POST",
		"/api/v3/repos/"+owner+"/"+repoName+"/pulls",
		bytes.NewBufferString(`{"title":"loop test","head":"feature","base":"main"}`))
	prReq.Header.Set("Authorization", "Bearer "+tok)
	prReq.Header.Set("Content-Type", "application/json")
	prRR := httptest.NewRecorder()
	r.ServeHTTP(prRR, prReq)
	if prRR.Code != http.StatusCreated {
		t.Fatalf("create pull: %d %s", prRR.Code, prRR.Body.String())
	}

	// Loop prevention must have suppressed all delivery tasks.
	if got := enqueuer.Drain(); len(got) != 0 {
		t.Errorf("expected 0 tasks (loop prevention), got %d", len(got))
	}
}

// seedBareRepoWithFeatureBranchLoop creates a bare repo with main + feature
// branches so CreatePull branch-existence checks pass.
func seedBareRepoWithFeatureBranchLoop(t *testing.T, root, owner, repo string) {
	t.Helper()
	// Delegate to the shared helper in e2e_webhook_test.go which has the same
	// signature. If it is renamed, update here.
	seedBareRepoWithFeatureBranchE2E(t, root, owner, repo)
}
