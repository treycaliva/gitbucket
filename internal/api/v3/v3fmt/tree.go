package v3fmt

import "fmt"

type TreeSource struct {
	Owner, Repo, SHA string
	Truncated        bool
	Entries          []TreeEntrySource
}

type TreeEntrySource struct {
	Path string
	Mode string
	Type string // "blob" | "tree" | "commit"
	SHA  string
	Size int64
}

type TreeEntryDTO struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size,omitempty"`
	URL  string `json:"url"`
}

type TreeDTO struct {
	SHA       string         `json:"sha"`
	URL       string         `json:"url"`
	Tree      []TreeEntryDTO `json:"tree"`
	Truncated bool           `json:"truncated"`
}

func Tree(src TreeSource, urls *URLBuilder) TreeDTO {
	entries := make([]TreeEntryDTO, 0, len(src.Entries))
	for _, e := range src.Entries {
		entries = append(entries, TreeEntryDTO{
			Path: e.Path,
			Mode: e.Mode,
			Type: e.Type,
			SHA:  e.SHA,
			Size: e.Size,
			URL:  fmt.Sprintf("%s/git/blobs/%s", urls.RepoAPI(src.Owner, src.Repo), e.SHA),
		})
	}
	return TreeDTO{
		SHA:       src.SHA,
		URL:       urls.GitTreeAPI(src.Owner, src.Repo, src.SHA),
		Tree:      entries,
		Truncated: src.Truncated,
	}
}
