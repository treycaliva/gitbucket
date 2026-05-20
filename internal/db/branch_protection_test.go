package db

import (
	"context"
	"testing"

	"gitbucket/internal/git"
)

func TestRuleCreateAndList(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()

	if err := CreateRepositoryMetadata(ctx, client, "owner-uid", "owner", "r1", "", "private"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	r := git.Rule{Pattern: "main", BlockForcePush: true, BlockDeletion: true}
	created, err := CreateRule(ctx, client, "owner", "r1", r)
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected ID to be set, got empty")
	}

	list, err := ListRules(ctx, client, "owner", "r1")
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	if len(list) != 1 || list[0].Pattern != "main" || !list[0].BlockForcePush {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestRuleGet(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()
	_ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r2", "", "private")

	r := git.Rule{Pattern: "release/*", RequirePullRequest: true, RequireApprovals: 2}
	created, err := CreateRule(ctx, client, "owner", "r2", r)
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	got, err := GetRule(ctx, client, "owner", "r2", created.ID)
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if got.Pattern != "release/*" || !got.RequirePullRequest || got.RequireApprovals != 2 {
		t.Fatalf("unexpected rule: %+v", got)
	}
}

func TestRuleUpdate(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()
	_ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r3", "", "private")

	r := git.Rule{Pattern: "main", RequireApprovals: 1}
	created, err := CreateRule(ctx, client, "owner", "r3", r)
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	created.RequireApprovals = 3
	created.BlockForcePush = true
	if err := UpdateRule(ctx, client, "owner", "r3", created); err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}
	got, err := GetRule(ctx, client, "owner", "r3", created.ID)
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if got.RequireApprovals != 3 || !got.BlockForcePush {
		t.Fatalf("update did not take effect: %+v", got)
	}
}

func TestRuleDelete(t *testing.T) {
	ctx := context.Background()
	client := newTestFirestore(t, ctx)
	defer client.Close()
	_ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r4", "", "private")

	r := git.Rule{Pattern: "main"}
	created, err := CreateRule(ctx, client, "owner", "r4", r)
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	if err := DeleteRule(ctx, client, "owner", "r4", created.ID); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	list, err := ListRules(ctx, client, "owner", "r4")
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list after delete, got %+v", list)
	}
}
