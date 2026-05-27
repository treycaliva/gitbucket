package api

import (
	"bytes"
	"os"
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
	}
}
