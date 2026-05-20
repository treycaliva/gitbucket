package git

import "testing"

func TestEnforcePushBlockForcePush(t *testing.T) {
	rules := []Rule{{Pattern: "main", BlockForcePush: true}}
	updates := []RefUpdate{{RefName: "refs/heads/main", OldSha: "abc", NewSha: "def", IsForce: true}}
	res := EnforcePush(rules, updates, "u1")
	if len(res.Rejected) != 1 {
		t.Fatalf("expected 1 reject, got %d", len(res.Rejected))
	}
}

func TestEnforcePushBlockDeletion(t *testing.T) {
	rules := []Rule{{Pattern: "main", BlockDeletion: true}}
	updates := []RefUpdate{{RefName: "refs/heads/main", OldSha: "abc", NewSha: "0000000000000000000000000000000000000000", IsDelete: true}}
	res := EnforcePush(rules, updates, "u1")
	if len(res.Rejected) != 1 {
		t.Fatal("expected 1 reject")
	}
}

func TestEnforcePushAllowlist(t *testing.T) {
	rules := []Rule{{Pattern: "main", PushAllowlist: []string{"u-good"}}}
	res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-good")
	if len(res.Rejected) != 0 {
		t.Fatal("u-good should be allowed")
	}
	res = EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-bad")
	if len(res.Rejected) != 1 {
		t.Fatal("u-bad should be rejected")
	}
}

func TestEnforcePushEmptyAllowlistMeansNoDirectPush(t *testing.T) {
	rules := []Rule{{Pattern: "main", PushAllowlist: []string{}}}
	res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-anybody")
	if len(res.Rejected) != 1 {
		t.Fatal("empty allowlist must reject all")
	}
}

func TestEnforcePushNonMatchingRefPasses(t *testing.T) {
	rules := []Rule{{Pattern: "main", BlockForcePush: true}}
	res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/feature/x", OldSha: "a", NewSha: "b", IsForce: true}}, "u")
	if len(res.Rejected) != 0 {
		t.Fatal("feature branch should not match main rule")
	}
}

func TestMatchRuleMostSpecific(t *testing.T) {
	rules := []Rule{{Pattern: "*"}, {Pattern: "release/*"}, {Pattern: "release/v1"}}
	m := MatchRule(rules, "release/v1")
	if m == nil || m.Pattern != "release/v1" {
		t.Fatalf("got %+v", m)
	}
}
