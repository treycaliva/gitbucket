// internal/apps/httpio_test.go
package apps

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteJSONSetsContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, 200, map[string]string{"hello": "world"})
	if got := rr.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if rr.Code != 200 {
		t.Errorf("code = %d", rr.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["hello"] != "world" {
		t.Errorf("body = %v", got)
	}
}

func TestWriteErrorGitHubShape(t *testing.T) {
	cases := []struct {
		err      error
		wantCode int
		wantMsg  string
	}{
		{ErrUnauthorized, 401, "Bad credentials"},
		{ErrNotFound, 404, "Not Found"},
		{ErrForbidden, 403, "Forbidden"},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		WriteError(rr, c.err)
		if rr.Code != c.wantCode {
			t.Errorf("code = %d, want %d", rr.Code, c.wantCode)
		}
		var body map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["message"] != c.wantMsg {
			t.Errorf("body.message = %q, want %q", body["message"], c.wantMsg)
		}
		if body["documentation_url"] == "" {
			t.Error("documentation_url should be set")
		}
	}
}

func TestWriteErrorUnknownIs500(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, errOtherForTest())
	if rr.Code != 500 {
		t.Errorf("code = %d, want 500", rr.Code)
	}
}

// errOtherForTest returns a generic non-typed error for the unknown-error path.
func errOtherForTest() error { return &genericError{"boom"} }

type genericError struct{ s string }

func (g *genericError) Error() string { return g.s }
