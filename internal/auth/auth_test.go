package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireWebAuthDevModeMockToken(t *testing.T) {
	handler := NewAuthHandler(true, nil, nil) // DevMode = true

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer mock_user_123")
	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := GetUID(r.Context())
		if uid != "user_123" {
			t.Errorf("expected UID to be user_123, got %s", uid)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler.RequireWebAuth(nextHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 OK, got %d", rr.Code)
	}
}

func TestRequireWebAuthMissingToken(t *testing.T) {
	handler := NewAuthHandler(true, nil, nil)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})

	handler.RequireWebAuth(nextHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized, got %d", rr.Code)
	}
}

func TestOptionalWebAuthDevModeMockToken(t *testing.T) {
	handler := NewAuthHandler(true, nil, nil)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer mock_user_123")
	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := GetUID(r.Context())
		if uid != "user_123" {
			t.Errorf("expected UID to be user_123, got %s", uid)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler.OptionalWebAuth(nextHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 OK, got %d", rr.Code)
	}
}

func TestOptionalWebAuthMissingToken(t *testing.T) {
	handler := NewAuthHandler(true, nil, nil)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := GetUID(r.Context())
		if uid != "" {
			t.Errorf("expected empty UID, got %s", uid)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler.OptionalWebAuth(nextHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 OK, got %d", rr.Code)
	}
}
