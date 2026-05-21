package apps

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubStore is a tiny SecretStore for testing the cache without hitting
// Secret Manager. Counts calls.
type stubStore struct {
	value    []byte
	err      error
	getCount int
}

func (s *stubStore) Put(_ context.Context, _ string, _ []byte) (string, error) {
	return "", nil
}
func (s *stubStore) Get(_ context.Context, _ string) ([]byte, error) {
	s.getCount++
	if s.err != nil {
		return nil, s.err
	}
	cp := make([]byte, len(s.value))
	copy(cp, s.value)
	return cp, nil
}
func (s *stubStore) Delete(_ context.Context, _ string) error { return nil }

func TestWebhookSecretCacheHitsStoreOnce(t *testing.T) {
	store := &stubStore{value: []byte("hunter2")}
	cache := NewWebhookSecretCache(store, 5*time.Minute)

	for i := 0; i < 3; i++ {
		got, err := cache.Get(context.Background(), "apps/x/webhook-secret")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != "hunter2" {
			t.Errorf("Get = %q", got)
		}
	}
	if store.getCount != 1 {
		t.Errorf("store.Get called %d times, want 1 (cache should reuse)", store.getCount)
	}
}

func TestWebhookSecretCacheExpiry(t *testing.T) {
	store := &stubStore{value: []byte("v1")}
	cache := NewWebhookSecretCache(store, 50*time.Millisecond)
	if _, err := cache.Get(context.Background(), "x"); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	store.value = []byte("v2")
	time.Sleep(60 * time.Millisecond)
	got, _ := cache.Get(context.Background(), "x")
	if string(got) != "v2" {
		t.Errorf("after expiry got %q, want v2 (cache should refresh)", got)
	}
}

func TestWebhookSecretCachePropagatesError(t *testing.T) {
	store := &stubStore{err: errors.New("boom")}
	cache := NewWebhookSecretCache(store, time.Minute)
	if _, err := cache.Get(context.Background(), "x"); err == nil {
		t.Error("expected error from underlying store to propagate")
	}
}
