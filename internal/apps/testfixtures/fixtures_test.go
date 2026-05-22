// internal/apps/testfixtures/fixtures_test.go
package testfixtures

import (
	"context"
	"os"
	"strings"
	"testing"

	"gitbucket/internal/apps"
	"gitbucket/internal/db"
)

func TestNewTestAppAndInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, _ := db.NewClient(ctx, "git-bucket-79382")
	defer fs.Close()

	scen := NewScenario(t, ctx, fs)
	defer scen.Cleanup(ctx)

	if scen.App == nil || scen.App.AppID == "" {
		t.Fatal("scenario should have an App")
	}
	if scen.Installation == nil {
		t.Fatal("scenario should have an Installation")
	}
	if scen.PrivateKey == nil {
		t.Fatal("scenario should expose the private key for signing JWTs")
	}

	jwt := scen.SignJWT(t)
	if !strings.HasPrefix(jwt, "ey") { // base64 header prefix for {"alg":...}
		t.Errorf("jwt does not look like a JWS: %q", jwt[:5])
	}

	tok, err := scen.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if !strings.HasPrefix(tok, "ghs_") {
		t.Errorf("token prefix wrong: %q", tok)
	}
}

// Compile-time check that the package exports match what Plan 2+ consumers need.
var _ *Scenario = (*Scenario)(nil)
var _ = apps.PermWrite // confirm apps package is importable
