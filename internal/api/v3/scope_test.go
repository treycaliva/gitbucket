package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

// TestRepoScopeEnforcement proves an installation token cannot reach
// repositories outside its installation: neither another account's repos nor
// repos excluded from a "selected" repository subset. Regression test for the
// cross-tenant gap where handlers checked only permission scopes.
func TestRepoScopeEnforcement(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	// Victim: installation with one selected repo.
	victim := testfixtures.NewScenario(t, ctx, fs)
	defer victim.Cleanup(ctx)
	victimRepo, err := victim.SeedRepo(ctx)
	if err != nil {
		t.Fatalf("SeedRepo(victim): %v", err)
	}
	// The second SeedRepo below replaces the fixture's tracked repo list, so
	// Cleanup would miss this first repo doc.
	t.Cleanup(func() {
		repoID := strings.ToLower(victim.Installation.Account.ID) + "_" + strings.ToLower(victimRepo)
		_, _ = fs.Collection("repositories").Doc(repoID).Delete(ctx)
	})
	victimTok, err := victim.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken(victim): %v", err)
	}

	// Attacker: a different app + installation on a different account.
	attacker := testfixtures.NewScenario(t, ctx, fs)
	defer attacker.Cleanup(ctx)
	attackerTok, err := attacker.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken(attacker): %v", err)
	}

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	RegisterV3Routes(r, h)

	get := func(token, owner, repo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repo, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	victimOwner := victim.Installation.Account.ID

	// Owner's token reaches its own repo.
	if rr := get(victimTok, victimOwner, victimRepo); rr.Code != http.StatusOK {
		t.Errorf("owner access: code = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}

	// Another installation's token gets 404 — not 200, and not 403 (which
	// would confirm the repo exists).
	if rr := get(attackerTok, victimOwner, victimRepo); rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant access: code = %d, want 404; body: %s", rr.Code, rr.Body.String())
	}

	// A write is refused the same way.
	req := httptest.NewRequest("POST", "/api/v3/repos/"+victimOwner+"/"+victimRepo+"/pulls", nil)
	req.Header.Set("Authorization", "Bearer "+attackerTok)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant write: code = %d, want 404; body: %s", rr.Code, rr.Body.String())
	}

	// A token minted for a repo subset that excludes the target also gets 404,
	// even though the installation account matches.
	out, err := apps.MintInstallationToken(ctx, fs, victim.Installation, apps.MintRequest{
		RepositoryIDs: victim.Installation.RepositoryIDs,
	})
	if err != nil {
		t.Fatalf("MintInstallationToken(subset): %v", err)
	}
	otherRepo, err := victim.SeedRepo(ctx) // second repo, not in the earlier token's subset
	if err != nil {
		t.Fatalf("SeedRepo(second): %v", err)
	}
	if rr := get(out.Plaintext, victimOwner, otherRepo); rr.Code != http.StatusNotFound {
		t.Errorf("subset exclusion: code = %d, want 404; body: %s", rr.Code, rr.Body.String())
	}
	// And the subset token still reaches the repo it was minted for.
	if rr := get(out.Plaintext, victimOwner, victimRepo); rr.Code != http.StatusOK {
		t.Errorf("subset inclusion: code = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
}
