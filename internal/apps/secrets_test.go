package apps

import (
	"context"
	"testing"
)

func TestMemorySecretStore_PutGetDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemorySecretStore()

	name, err := s.Put(ctx, "apps/test/key", []byte("hello"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if name == "" {
		t.Fatal("expected non-empty resource name")
	}

	got, err := s.Get(ctx, name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("Get returned %q, want %q", got, "hello")
	}

	if err := s.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, name); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestMemorySecretStore_GetUnknown(t *testing.T) {
	s := NewMemorySecretStore()
	if _, err := s.Get(context.Background(), "nope"); err == nil {
		t.Error("expected error for unknown resource")
	}
}
