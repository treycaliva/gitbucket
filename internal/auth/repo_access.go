package auth

import "gitbucket/internal/db"

// RepoAccess captures whether a user is the owner or a collaborator.
type RepoAccess struct {
	IsOwner        bool
	IsCollaborator bool
}

// HasRepoAccess returns the access tuple for the given uid against the repo metadata.
func HasRepoAccess(meta *db.RepositoryMetadata, uid string) RepoAccess {
	if meta == nil || uid == "" {
		return RepoAccess{}
	}
	if meta.OwnerUID == uid {
		return RepoAccess{IsOwner: true}
	}
	for _, c := range meta.Collaborators {
		if c.UID == uid {
			return RepoAccess{IsCollaborator: true}
		}
	}
	return RepoAccess{}
}

// CanPush is true when the user is the owner or a collaborator.
// Branch protection layers narrow this further at push-time.
func CanPush(meta *db.RepositoryMetadata, uid string) bool {
	ra := HasRepoAccess(meta, uid)
	return ra.IsOwner || ra.IsCollaborator
}

// CanRead is true when the repo is public, or the user has any access.
func CanRead(meta *db.RepositoryMetadata, uid string) bool {
	if meta == nil {
		return false
	}
	if meta.Visibility == "public" {
		return true
	}
	return CanPush(meta, uid)
}
