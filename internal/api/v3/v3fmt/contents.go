package v3fmt

import (
	"encoding/base64"
)

// ContentFileSource carries inputs needed to render a file's contents per
// GitHub's `contents` API response.
type ContentFileSource struct {
	Owner, Repo, Path, Ref string
	SHA                    string
	Size                   int64
	Raw                    []byte
}

// ContentFileDTO matches GitHub's response for `GET /repos/.../contents/{path}`
// when the path is a file.
type ContentFileDTO struct {
	Type        string `json:"type"`     // always "file"
	Encoding    string `json:"encoding"` // always "base64"
	Size        int64  `json:"size"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Content     string `json:"content"` // base64 of Raw
	SHA         string `json:"sha"`
	URL         string `json:"url"`
	HTMLURL     string `json:"html_url"`
	DownloadURL string `json:"download_url"`
}

func ContentFile(src ContentFileSource, urls *URLBuilder) ContentFileDTO {
	return ContentFileDTO{
		Type:        "file",
		Encoding:    "base64",
		Size:        src.Size,
		Name:        baseName(src.Path),
		Path:        src.Path,
		Content:     base64.StdEncoding.EncodeToString(src.Raw),
		SHA:         src.SHA,
		URL:         urls.ContentsAPI(src.Owner, src.Repo, src.Path, src.Ref),
		HTMLURL:     urls.RepoHTML(src.Owner, src.Repo) + "/blob/" + src.Ref + "/" + src.Path,
		DownloadURL: urls.RepoHTML(src.Owner, src.Repo) + "/raw/" + src.Ref + "/" + src.Path,
	}
}

// ContentDirSource carries directory listing inputs.
type ContentDirSource struct {
	Owner, Repo, Path, Ref string
	Entries                []DirEntry
}

type DirEntry struct {
	Name string
	Path string
	SHA  string
	Type string // "file" | "dir"
	Size int64  // 0 for dirs
}

// ContentDirEntryDTO is the GitHub shape for an entry in a directory listing.
type ContentDirEntryDTO struct {
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	SHA         string `json:"sha"`
	URL         string `json:"url"`
	HTMLURL     string `json:"html_url"`
	DownloadURL string `json:"download_url,omitempty"`
}

func ContentDir(src ContentDirSource, urls *URLBuilder) []ContentDirEntryDTO {
	out := make([]ContentDirEntryDTO, 0, len(src.Entries))
	for _, e := range src.Entries {
		entry := ContentDirEntryDTO{
			Type:    e.Type,
			Size:    e.Size,
			Name:    e.Name,
			Path:    e.Path,
			SHA:     e.SHA,
			URL:     urls.ContentsAPI(src.Owner, src.Repo, e.Path, src.Ref),
			HTMLURL: urls.RepoHTML(src.Owner, src.Repo) + "/blob/" + src.Ref + "/" + e.Path,
		}
		if e.Type == "file" {
			entry.DownloadURL = urls.RepoHTML(src.Owner, src.Repo) + "/raw/" + src.Ref + "/" + e.Path
		}
		out = append(out, entry)
	}
	return out
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
