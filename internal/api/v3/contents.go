package v3

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gitbucket/internal/api/v3/v3fmt"
	"gitbucket/internal/apps"
)

// GetContents handles GET /api/v3/repos/{owner}/{repo}/contents/{path}.
// Supports both file (returns one ContentFileDTO) and directory (returns
// array of ContentDirEntryDTO).
func (h *V3Handler) GetContents(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "contents", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	path := chi.URLParam(r, "*")
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = "main"
	}

	bare, err := MaterializeRepo(r.Context(), h.FirestoreClient, h.StorageClient, owner, repo, h.LocalReposRoot)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			apps.WriteError(w, apps.ErrNotFound)
			return
		}
		apps.WriteError(w, err)
		return
	}

	// First try directory listing via git ls-tree.
	// lsTree returns (nil, false, err) when git ls-tree fails (e.g., because
	// the path is a file blob, not a tree object). In that case fall through
	// to file mode rather than immediately returning 404.
	entries, isDir, lsErr := lsTree(bare, ref, path)
	if lsErr == nil && isDir {
		apps.WriteJSON(w, http.StatusOK, v3fmt.ContentDir(v3fmt.ContentDirSource{
			Owner: owner, Repo: repo, Path: path, Ref: ref, Entries: entries,
		}, h.URLs))
		return
	}

	// Not a dir (or ls-tree failed because path is a blob) — try file via git show.
	raw, sha, size, err := gitShowFile(bare, ref, path)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, v3fmt.ContentFile(v3fmt.ContentFileSource{
		Owner: owner, Repo: repo, Path: path, Ref: ref,
		SHA: sha, Size: size, Raw: raw,
	}, h.URLs))
}

// lsTree runs `git ls-tree` against the bare repo. If `path` is empty or
// refers to a directory, returns (entries, true, nil). If `path` refers to
// a single file (one ls-tree row matching the exact path), returns
// (nil, false, nil) to signal the caller to fall through to file mode.
func lsTree(bare, ref, path string) ([]v3fmt.DirEntry, bool, error) {
	target := ref
	if path != "" {
		target = ref + ":" + path
	}
	cmd := exec.Command("git", "--git-dir", bare, "ls-tree", "-l", "--full-name", target)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, false, err
	}

	var entries []v3fmt.DirEntry
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		fields := strings.Fields(parts[0])
		if len(fields) < 4 {
			continue
		}
		typ := fields[1]
		sha := fields[2]
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		p := parts[1]
		ftype := "file"
		if typ == "tree" {
			ftype = "dir"
		}
		entries = append(entries, v3fmt.DirEntry{
			Name: filepathBase(p), Path: p, SHA: sha, Type: ftype, Size: size,
		})
	}

	// Heuristic: if a single entry path equals exactly `path`, this was a
	// file lookup (not a dir). Signal caller to use gitShowFile instead.
	if path != "" && len(entries) == 1 && entries[0].Path == path {
		return nil, false, nil
	}

	if entries == nil {
		entries = []v3fmt.DirEntry{}
	}
	return entries, true, nil
}

func gitShowFile(bare, ref, path string) (raw []byte, sha string, size int64, err error) {
	target := ref + ":" + path
	cmd := exec.Command("git", "--git-dir", bare, "show", target)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if e := cmd.Run(); e != nil {
		return nil, "", 0, e
	}
	raw = out.Bytes()
	size = int64(len(raw))

	// SHA via `git rev-parse <target>`.
	cmd2 := exec.Command("git", "--git-dir", bare, "rev-parse", target)
	var sha2 bytes.Buffer
	cmd2.Stdout = &sha2
	if e := cmd2.Run(); e == nil {
		sha = strings.TrimSpace(sha2.String())
	}
	return raw, sha, size, nil
}

func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
