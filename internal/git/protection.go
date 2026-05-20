package git

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// zeroSha is the canonical "deletion" sentinel in Git's wire protocol.
const zeroSha = "0000000000000000000000000000000000000000"

// LocalRefs returns the current `refs/heads/*` map of the bare repo at repoPath.
// Keys are full refs (refs/heads/main), values are 40-char SHAs.
// If git fails (e.g. uninitialized repo), an empty map is returned with no error
// — that's the correct state for "no branches yet".
func LocalRefs(repoPath string) map[string]string {
	out := map[string]string{}
	cmd := exec.Command("git", "--git-dir", repoPath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		out[parts[0]] = parts[1]
	}
	return out
}

// IsAncestor reports whether oldSha is an ancestor of newSha in the repo at repoPath.
// A missing or zero oldSha is treated as an ancestor (i.e. branch creation is not a force-push).
func IsAncestor(repoPath, oldSha, newSha string) bool {
	if oldSha == "" || oldSha == zeroSha {
		return true
	}
	cmd := exec.Command("git", "--git-dir", repoPath, "merge-base", "--is-ancestor", oldSha, newSha)
	return cmd.Run() == nil
}

// DiffRefs compares pre-push and post-push refs/heads/* maps and returns a RefUpdate
// for every changed ref. Force-push and deletion flags are computed via IsAncestor /
// zero-SHA sentinel.
func DiffRefs(repoPath string, before, after map[string]string) []RefUpdate {
	var out []RefUpdate
	// Updates and deletions.
	for ref, oldSha := range before {
		newSha, stillPresent := after[ref]
		if !stillPresent {
			out = append(out, RefUpdate{
				RefName:  ref,
				OldSha:   oldSha,
				NewSha:   zeroSha,
				IsDelete: true,
			})
			continue
		}
		if newSha == oldSha {
			continue
		}
		out = append(out, RefUpdate{
			RefName: ref,
			OldSha:  oldSha,
			NewSha:  newSha,
			IsForce: !IsAncestor(repoPath, oldSha, newSha),
		})
	}
	// New branches.
	for ref, newSha := range after {
		if _, existed := before[ref]; existed {
			continue
		}
		out = append(out, RefUpdate{
			RefName: ref,
			OldSha:  "",
			NewSha:  newSha,
		})
	}
	return out
}

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

// EnforcePush evaluates each ref update against the rule set and returns the rejections.
// "branch" is derived from RefName by stripping refs/heads/. Refs outside refs/heads/
// are not gated by branch rules in v1.
func EnforcePush(rules []Rule, updates []RefUpdate, pusherUid string) EnforceResult {
	out := EnforceResult{Reasons: map[string]string{}}
	for _, u := range updates {
		branch := strings.TrimPrefix(u.RefName, "refs/heads/")
		if branch == u.RefName {
			// not a branch (e.g. tag) — skip in v1
			continue
		}
		rule := MatchRule(rules, branch)
		if rule == nil {
			continue
		}

		if u.IsDelete && rule.BlockDeletion {
			out.Rejected = append(out.Rejected, u)
			out.Reasons[u.RefName] = "branch deletion is blocked by protection rule"
			continue
		}
		if u.IsForce && rule.BlockForcePush {
			out.Rejected = append(out.Rejected, u)
			out.Reasons[u.RefName] = "force-push is blocked by protection rule"
			continue
		}
		if !inAllowlist(rule.PushAllowlist, pusherUid) {
			out.Rejected = append(out.Rejected, u)
			out.Reasons[u.RefName] = "direct push to this branch is not allowed"
			continue
		}
	}
	return out
}

// inAllowlist reports whether uid is in the explicit allowlist.
// An empty list means nobody is allowed (use this to require PRs).
func inAllowlist(list []string, uid string) bool {
	for _, x := range list {
		if x == uid {
			return true
		}
	}
	return false
}
