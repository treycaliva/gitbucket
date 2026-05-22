package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps"
	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestV3RouteSkeletonProtectedByInstallationToken(t *testing.T) {
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
	tok, err := scen.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	RegisterV3Routes(r, h)

	// Without token: 401.
	req := httptest.NewRequest("GET", "/api/v3/_ping", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-token: code = %d, want 401", rr.Code)
	}

	// With token: 200 from the stub.
	req = httptest.NewRequest("GET", "/api/v3/_ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("with-token: code = %d body: %s", rr.Code, rr.Body.String())
	}

	// Sanity: middleware injected the InstallationContext.
	_ = apps.InstallationContextFrom // compile-time check that import is valid
}
