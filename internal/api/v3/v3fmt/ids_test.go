package v3fmt

import "testing"

func TestStableIDDeterministic(t *testing.T) {
	a := StableID("user:alice")
	b := StableID("user:alice")
	if a != b {
		t.Errorf("StableID not deterministic: %d vs %d", a, b)
	}
}

func TestStableIDDifferentInputs(t *testing.T) {
	if StableID("foo") == StableID("bar") {
		t.Error("StableID collided on trivial inputs")
	}
}

func TestStableIDPositive(t *testing.T) {
	for _, in := range []string{"a", "", "alice/repo", "issue:repo:42"} {
		if got := StableID(in); got < 0 {
			t.Errorf("StableID(%q) = %d, want non-negative", in, got)
		}
	}
}
