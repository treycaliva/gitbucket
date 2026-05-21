package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/apps/testfixtures"
	"gitbucket/internal/db"
)

func TestGetRepo(t *testing.T) {
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
	tok, _ := scen.MintToken(ctx)

	owner := scen.Installation.Account.ID
	repoName := "demo-" + randHex(4)
	if err := db.CreateRepositoryMetadata(ctx, fs, owner, owner, repoName, "demo desc", "public"); err != nil {
		t.Fatalf("CreateRepositoryMetadata: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DeleteRepositoryMetadata(context.Background(), fs, owner, repoName)
	})

	r := chi.NewRouter()
	h := NewV3Handler(fs, nil, "https://test.gitbucket.local")
	RegisterV3Routes(r, h)

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d body: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["name"] != repoName {
			t.Errorf("name = %v, want %s", body["name"], repoName)
		}
		if body["full_name"] != owner+"/"+repoName {
			t.Errorf("full_name = %v, want %s/%s", body["full_name"], owner, repoName)
		}
		if body["node_id"] == nil || body["id"] == nil {
			t.Errorf("missing id/node_id: %+v", body)
		}
		owmap, ok := body["owner"].(map[string]interface{})
		if !ok {
			t.Fatalf("owner missing or wrong type: %+v", body["owner"])
		}
		if owmap["login"] != owner {
			t.Errorf("owner.login = %v, want %s", owmap["login"], owner)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/no-such-repo", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404", rr.Code)
		}
	})

	t.Run("no token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/repos/"+owner+"/"+repoName, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("code = %d, want 401", rr.Code)
		}
	})
}
