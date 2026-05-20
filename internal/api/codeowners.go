package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
)

// codeOwnersResponse is the JSON shape returned by CodeOwnersHandler.
// Keys are entry names within the requested directory; values are the owner
// handles (e.g. "@alice") matched by the resolved CODEOWNERS rules. Entries
// with no matching rule are omitted.
type codeOwnersResponse struct {
	Entries map[string][]string `json:"entries"`
}

// CodeOwnersHandler handles GET /api/repos/{owner}/{repo}/codeowners?path={dir}&ref={sha}.
//
// Access: same read-access semantics as the tree/blob endpoints (delegated to
// authorizeGitRead). The handler resolves CODEOWNERS from the materialized bare
// repository at the given ref (defaulting to the repo's default branch when
// omitted) and returns the matched owner handles for each immediate child of
// the requested directory.
func (h *APIHandler) CodeOwnersHandler(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")

	meta, localRepoPath, ok := h.authorizeGitRead(&w, r, owner, repo)
	if !ok {
		return
	}

	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		if meta != nil && meta.DefaultBranch != "" {
			ref = meta.DefaultBranch
		} else {
			ref = "main"
		}
	}

	dir := strings.Trim(r.URL.Query().Get("path"), "/")

	// List immediate children of dir at ref via `git ls-tree`. We only need
	// the name/type fields for matching, so use the non-`-l` form.
	var target string
	if dir == "" {
		target = ref
	} else {
		target = ref + ":" + dir
	}

	cmd := exec.Command("git", "--git-dir", localRepoPath, "ls-tree", "--name-only", "--full-tree", target)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errStr := strings.ToLower(stderr.String())
		// Empty repo / missing dir / unknown ref → return an empty map rather
		// than a 5xx so the UI can fall back gracefully.
		if strings.Contains(errStr, "not a valid object name") ||
			strings.Contains(errStr, "does not exist") ||
			strings.Contains(errStr, "bad default revision") {
			writeCodeOwnersJSON(w, map[string][]string{})
			return
		}
		http.Error(w, "Git error: "+stderr.String(), http.StatusInternalServerError)
		return
	}

	// Load CODEOWNERS from the same ref. Resolution order (root,
	// .gitbucket/CODEOWNERS, docs/CODEOWNERS) is handled by the helper.
	co, err := loadCodeOwnersFromBareRepo(localRepoPath, ref)
	if err != nil {
		// Treat parse/load errors as "no rules" so the file listing still
		// renders. The error is non-fatal for browsing.
		co = nil
	}

	entries := map[string][]string{}
	if co != nil {
		scanner := bufio.NewScanner(&stdout)
		for scanner.Scan() {
			name := strings.TrimSpace(scanner.Text())
			if name == "" {
				continue
			}
			full := name
			if dir != "" {
				full = path.Join(dir, name)
			}
			matched := co.Match(full)
			if len(matched) == 0 {
				continue
			}
			// Defensive copy so future Match() calls (if Match returns an
			// internal slice) can't mutate prior entries.
			out := make([]string, len(matched))
			copy(out, matched)
			entries[name] = out
		}
	}

	writeCodeOwnersJSON(w, entries)
}

func writeCodeOwnersJSON(w http.ResponseWriter, entries map[string][]string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(codeOwnersResponse{Entries: entries})
}
