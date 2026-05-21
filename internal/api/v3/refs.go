package v3

import (
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

func (h *V3Handler) GetRef(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "contents", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "*")

	bare, err := MaterializeRepo(r.Context(), h.FirestoreClient, h.StorageClient, owner, repo, h.LocalReposRoot)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			apps.WriteError(w, apps.ErrNotFound)
			return
		}
		apps.WriteError(w, err)
		return
	}

	sha, err := gitRevParse(bare, "refs/"+ref)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, v3fmt.Ref(v3fmt.RefSource{
		Owner: owner, Repo: repo, Ref: ref, SHA: sha,
	}, h.URLs))
}

func (h *V3Handler) GetTree(w http.ResponseWriter, r *http.Request) {
	if err := apps.RequirePerm(r.Context(), "contents", apps.PermRead); err != nil {
		apps.WriteError(w, err)
		return
	}
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")
	recursive := r.URL.Query().Get("recursive") == "1" || r.URL.Query().Get("recursive") == "true"

	bare, err := MaterializeRepo(r.Context(), h.FirestoreClient, h.StorageClient, owner, repo, h.LocalReposRoot)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			apps.WriteError(w, apps.ErrNotFound)
			return
		}
		apps.WriteError(w, err)
		return
	}

	entries, err := gitListTree(bare, sha, recursive)
	if err != nil {
		apps.WriteError(w, apps.ErrNotFound)
		return
	}
	apps.WriteJSON(w, http.StatusOK, v3fmt.Tree(v3fmt.TreeSource{
		Owner: owner, Repo: repo, SHA: sha, Entries: entries, Truncated: false,
	}, h.URLs))
}

func gitRevParse(bare, target string) (string, error) {
	cmd := exec.Command("git", "--git-dir", bare, "rev-parse", target)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func gitListTree(bare, sha string, recursive bool) ([]v3fmt.TreeEntrySource, error) {
	args := []string{"--git-dir", bare, "ls-tree", "-l"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, sha)
	cmd := exec.Command("git", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	var entries []v3fmt.TreeEntrySource
	for _, line := range strings.Split(out.String(), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		fields := strings.Fields(parts[0])
		if len(fields) < 4 {
			continue
		}
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		entries = append(entries, v3fmt.TreeEntrySource{
			Mode: fields[0],
			Type: fields[1], // "blob" | "tree" | "commit"
			SHA:  fields[2],
			Size: size,
			Path: parts[1],
		})
	}
	return entries, nil
}
