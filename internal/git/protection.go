package git

import (
	"path/filepath"
	"sort"
	"strings"
)

// Rule is a single branch-protection rule attached to a repository.
// `Pattern` is matched against branch short-names using filepath.Match semantics.
type Rule struct {
	ID                       string   `json:"id" firestore:"id"`
	Pattern                  string   `json:"pattern" firestore:"pattern"`
	PushAllowlist            []string `json:"pushAllowlist" firestore:"pushAllowlist"`
	MergeAllowlist           []string `json:"mergeAllowlist" firestore:"mergeAllowlist"`
	RequirePullRequest       bool     `json:"requirePullRequest" firestore:"requirePullRequest"`
	RequireApprovals         int      `json:"requireApprovals" firestore:"requireApprovals"`
	RequireCodeownerApproval bool     `json:"requireCodeownerApproval" firestore:"requireCodeownerApproval"`
	BlockForcePush           bool     `json:"blockForcePush" firestore:"blockForcePush"`
	BlockDeletion            bool     `json:"blockDeletion" firestore:"blockDeletion"`
}

// RefUpdate is a single proposed ref change observed during a push.
type RefUpdate struct {
	RefName  string // e.g. "refs/heads/main"
	OldSha   string
	NewSha   string
	IsForce  bool
	IsDelete bool
}

// EnforceResult is the verdict returned by EnforcePush.
type EnforceResult struct {
	Rejected []RefUpdate
	Reasons  map[string]string // ref → human reason
}

// MatchRule returns the most-specific rule matching the branch name, or nil.
// Branch is the short name (e.g. "main"), not the full ref.
//
// "Most specific" is defined as:
//  1. fewer wildcards wins,
//  2. longer pattern wins (tie-breaker on equal wildcard counts),
//  3. lexicographic pattern order for full determinism.
func MatchRule(rules []Rule, branch string) *Rule {
	var matched []Rule
	for _, r := range rules {
		ok, _ := filepath.Match(r.Pattern, branch)
		if ok {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	sort.SliceStable(matched, func(i, j int) bool {
		ai, aj := wildcardCount(matched[i].Pattern), wildcardCount(matched[j].Pattern)
		if ai != aj {
			return ai < aj
		}
		if len(matched[i].Pattern) != len(matched[j].Pattern) {
			return len(matched[i].Pattern) > len(matched[j].Pattern)
		}
		return matched[i].Pattern < matched[j].Pattern
	})
	return &matched[0]
}

func wildcardCount(s string) int {
	return strings.Count(s, "*") + strings.Count(s, "?") + strings.Count(s, "[")
}
