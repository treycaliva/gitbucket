package v3fmt

import "fmt"

type RefSource struct {
	Owner, Repo, Ref, SHA string
}

type RefObjectDTO struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

type RefDTO struct {
	Ref    string       `json:"ref"`
	NodeID string       `json:"node_id"`
	URL    string       `json:"url"`
	Object RefObjectDTO `json:"object"`
}

func Ref(src RefSource, urls *URLBuilder) RefDTO {
	fullRef := "refs/" + src.Ref
	nodeKey := fmt.Sprintf("%s/%s:%s", src.Owner, src.Repo, src.Ref)
	return RefDTO{
		Ref:    fullRef,
		NodeID: encodeNodeID("Ref", StableID(nodeKey)),
		URL:    urls.GitRefAPI(src.Owner, src.Repo, src.Ref),
		Object: RefObjectDTO{
			Type: "commit",
			SHA:  src.SHA,
			URL:  fmt.Sprintf("%s/git/commits/%s", urls.RepoAPI(src.Owner, src.Repo), src.SHA),
		},
	}
}
