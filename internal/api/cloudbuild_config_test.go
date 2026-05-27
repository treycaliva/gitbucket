package api

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// cbStep mirrors the EXACT set of fields the engine honors in TriggerCloudBuild.
// Decoding with KnownFields(true) therefore fails if cloudbuild.yaml relies on
// any field the engine silently ignores (e.g. a top-level `images:` or `options:`).
type cbStep struct {
	Name       string   `yaml:"name"`
	Entrypoint string   `yaml:"entrypoint"`
	Args       []string `yaml:"args"`
	Env        []string `yaml:"env"`
	Dir        string   `yaml:"dir"`
	ID         string   `yaml:"id"`
	WaitFor    []string `yaml:"waitFor"`
}

type cbConfig struct {
	Steps []cbStep `yaml:"steps"`
}

// loadCloudBuildConfig reads the repo-root cloudbuild.yaml relative to this
// package directory (internal/api) and strict-decodes it.
func loadCloudBuildConfig(t *testing.T) cbConfig {
	t.Helper()
	data, err := os.ReadFile("../../cloudbuild.yaml")
	if err != nil {
		t.Fatalf("reading cloudbuild.yaml: %v", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg cbConfig
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("cloudbuild.yaml uses fields the engine does not honor (or is invalid): %v", err)
	}
	return cfg
}

func stepByID(cfg cbConfig, id string) (cbStep, bool) {
	for _, s := range cfg.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return cbStep{}, false
}

func TestCloudBuildYAMLDeployStepsAreBranchGuarded(t *testing.T) {
	cfg := loadCloudBuildConfig(t)

	const imageRef = "us-central1-docker.pkg.dev/git-bucket-79382/gitbucket/gitbucket:${_COMMIT_SHA}"
	const guard = `"${_BRANCH_NAME}" != "main"`

	deployIDs := []string{"image", "backend-deploy", "frontend-deploy"}
	for _, id := range deployIDs {
		step, ok := stepByID(cfg, id)
		if !ok {
			t.Fatalf("expected a deploy step with id %q", id)
		}
		script := strings.Join(step.Args, "\n")
		if !strings.Contains(script, guard) {
			t.Errorf("deploy step %q must be branch-guarded with %s so it no-ops off main", id, guard)
		}
	}

	// The image build, the backend deploy, and the pushed/deployed image must all
	// reference the same SHA-tagged Artifact Registry path.
	for _, id := range []string{"image", "backend-deploy"} {
		step, _ := stepByID(cfg, id)
		script := strings.Join(step.Args, "\n")
		if !strings.Contains(script, imageRef) {
			t.Errorf("step %q must reference the SHA-tagged image %q", id, imageRef)
		}
	}

	// CI steps must NOT carry the branch guard — they run on every branch.
	for _, id := range []string{"backend-compile", "frontend-build"} {
		step, _ := stepByID(cfg, id)
		script := strings.Join(step.Args, "\n")
		if strings.Contains(script, guard) {
			t.Errorf("CI step %q must not be branch-guarded", id)
		}
	}
}

func TestCloudBuildYAMLOnlyUsesHonoredFieldsAndHasCISteps(t *testing.T) {
	cfg := loadCloudBuildConfig(t)

	for _, id := range []string{"backend-compile", "frontend-build"} {
		step, ok := stepByID(cfg, id)
		if !ok {
			t.Fatalf("expected a step with id %q", id)
		}
		if step.Name == "" {
			t.Errorf("step %q has no image name", id)
		}
		if len(step.Args) == 0 {
			t.Errorf("step %q has no args", id)
		}
	}
}
