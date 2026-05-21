package apps

import (
	"context"
	"os"
	"testing"

	"gitbucket/internal/db"
)

func TestCreateAndGetInstallation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	ctx := context.Background()
	fs, err := db.NewClient(ctx, "git-bucket-79382")
	if err != nil {
		t.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()
	store := NewMemorySecretStore()

	slug := "inst-app-" + randHex(4)
	owner := AccountRef{ID: "owner-" + randHex(2), Type: AccountTypeUser}
	var botUID string
	var inst *Installation
	var app *App

	t.Cleanup(func() {
		cctx := context.Background()
		if inst != nil {
			_, _ = fs.Collection(CollectionInstallations).Doc(inst.InstallationID).Delete(cctx)
		}
		if app != nil {
			_, _ = fs.Collection(CollectionApps).Doc(app.AppID).Delete(cctx)
		}
		_, _ = fs.Collection("usernames").Doc(slug + "[bot]").Delete(cctx)
		if botUID != "" {
			_, _ = fs.Collection(CollectionUsers).Doc(botUID).Delete(cctx)
		}
	})

	botUID, err = CreateBotUser(ctx, fs, slug, slug, "", "pending")
	if err != nil {
		t.Fatalf("CreateBotUser: %v", err)
	}
	app, _, err = CreateApp(ctx, fs, store, CreateAppRequest{
		Slug: slug, Name: slug, OwnerAccount: owner, BotUserID: botUID,
		WebhookURL:         "https://example.com/x",
		DefaultPermissions: Permissions{"issues": PermWrite},
		DefaultEvents:      []string{"issues"},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	installeeAcct := AccountRef{ID: "acct-" + randHex(2), Type: AccountTypeUser}
	inst, err = CreateInstallation(ctx, fs, CreateInstallationRequest{
		AppID:               app.AppID,
		Account:             installeeAcct,
		RepositorySelection: "selected",
		RepositoryIDs:       []string{"repo1", "repo2"},
		Permissions:         Permissions{"issues": PermWrite},
		Events:              []string{"issues"},
	})
	if err != nil {
		t.Fatalf("CreateInstallation: %v", err)
	}
	if inst.InstallationID == "" {
		t.Fatal("InstallationID should be set")
	}

	got, err := GetInstallation(ctx, fs, inst.InstallationID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if got.AppID != app.AppID {
		t.Errorf("AppID = %s, want %s", got.AppID, app.AppID)
	}
	if len(got.RepositoryIDs) != 2 {
		t.Errorf("RepositoryIDs len = %d, want 2", len(got.RepositoryIDs))
	}

	// GetInstallationForApp enforces ownership.
	got2, err := GetInstallationForApp(ctx, fs, inst.InstallationID, app.AppID)
	if err != nil || got2 == nil {
		t.Fatalf("GetInstallationForApp same app: err=%v inst=%v", err, got2)
	}
	got3, err := GetInstallationForApp(ctx, fs, inst.InstallationID, "wrong-app-id")
	if err != nil {
		t.Fatalf("GetInstallationForApp wrong app err: %v", err)
	}
	if got3 != nil {
		t.Error("GetInstallationForApp should return nil for wrong app")
	}

	// ListInstallationsForApp finds it.
	list, err := ListInstallationsForApp(ctx, fs, app.AppID)
	if err != nil {
		t.Fatalf("ListInstallationsForApp: %v", err)
	}
	if len(list) != 1 || list[0].InstallationID != inst.InstallationID {
		t.Errorf("list = %+v", list)
	}
}
