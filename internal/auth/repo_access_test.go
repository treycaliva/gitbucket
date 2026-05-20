package auth

import (
	"testing"

	"gitbucket/internal/db"
)

func TestCanPushOwner(t *testing.T) {
	meta := &db.RepositoryMetadata{OwnerUID: "u1"}
	if !CanPush(meta, "u1") {
		t.Fatal("owner should be able to push")
	}
}

func TestCanPushCollaborator(t *testing.T) {
	meta := &db.RepositoryMetadata{
		OwnerUID:      "u1",
		Collaborators: []db.Collaborator{{UID: "u2", Username: "alice"}},
	}
	if !CanPush(meta, "u2") {
		t.Fatal("collaborator should be able to push")
	}
	if CanPush(meta, "u3") {
		t.Fatal("stranger should not be able to push")
	}
}

func TestCanReadPublic(t *testing.T) {
	meta := &db.RepositoryMetadata{Visibility: "public"}
	if !CanRead(meta, "") {
		t.Fatal("public should be readable by anyone")
	}
}

func TestCanReadPrivateRequiresAccess(t *testing.T) {
	meta := &db.RepositoryMetadata{Visibility: "private", OwnerUID: "u1"}
	if CanRead(meta, "u2") {
		t.Fatal("stranger cannot read private")
	}
	if !CanRead(meta, "u1") {
		t.Fatal("owner can read private")
	}
}
