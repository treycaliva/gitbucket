package api

import (
	"testing"

	cloudbuild "google.golang.org/api/cloudbuild/v1"
)

func TestAssembleBuildSetsAllowLooseAndSubstitutions(t *testing.T) {
	steps := []*cloudbuild.BuildStep{{Name: "alpine", Id: "s1"}}

	b := assembleBuild(steps, "owner1", "repo1", "main", "abc123", "https://gitbucket.dev", "")

	if b.Options == nil || b.Options.SubstitutionOption != "ALLOW_LOOSE" {
		t.Fatalf("expected Options.SubstitutionOption=ALLOW_LOOSE, got %+v", b.Options)
	}
	if len(b.Steps) != 1 || b.Steps[0].Id != "s1" {
		t.Errorf("steps not passed through: %+v", b.Steps)
	}
	want := map[string]string{
		"_COMMIT_SHA":    "abc123",
		"_BRANCH_NAME":   "main",
		"_REPO_OWNER":    "owner1",
		"_REPO_NAME":     "repo1",
		"_GITBUCKET_URL": "https://gitbucket.dev",
	}
	for k, v := range want {
		if b.Substitutions[k] != v {
			t.Errorf("substitution %q = %q, want %q", k, b.Substitutions[k], v)
		}
	}
	if b.ServiceAccount != "" {
		t.Errorf("expected empty ServiceAccount when none provided, got %q", b.ServiceAccount)
	}
}

func TestAssembleBuildSetsServiceAccountWhenProvided(t *testing.T) {
	b := assembleBuild(nil, "o", "r", "main", "sha", "https://x", "sa@project.iam.gserviceaccount.com")
	if b.ServiceAccount != "sa@project.iam.gserviceaccount.com" {
		t.Errorf("ServiceAccount = %q, want the provided SA", b.ServiceAccount)
	}
}
