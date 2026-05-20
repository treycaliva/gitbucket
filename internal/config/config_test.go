package config

import (
	"os"
	"testing"
)

func TestConfigLoad(t *testing.T) {
	// Set test environment variables
	os.Setenv("PORT", "9090")
	os.Setenv("GCS_BUCKET", "my-test-bucket")
	os.Setenv("DEV_MODE", "true")
	os.Setenv("RESTRICTED_IP", "127.0.0.1")
	os.Setenv("PROJECT_ID", "test-project")

	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("GCS_BUCKET")
		os.Unsetenv("DEV_MODE")
		os.Unsetenv("RESTRICTED_IP")
		os.Unsetenv("PROJECT_ID")
	}()

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("expected Port to be 9090, got %s", cfg.Port)
	}
	if cfg.GCSBucket != "my-test-bucket" {
		t.Errorf("expected GCSBucket to be my-test-bucket, got %s", cfg.GCSBucket)
	}
	if !cfg.DevMode {
		t.Errorf("expected DevMode to be true, got false")
	}
	if cfg.RestrictedIP != "127.0.0.1" {
		t.Errorf("expected RestrictedIP to be 127.0.0.1, got %s", cfg.RestrictedIP)
	}
	if cfg.ProjectID != "test-project" {
		t.Errorf("expected ProjectID to be test-project, got %s", cfg.ProjectID)
	}
}

func TestConfigLoadDefaults(t *testing.T) {
	os.Unsetenv("PORT")
	os.Unsetenv("GCS_BUCKET")
	os.Unsetenv("DEV_MODE")
	os.Unsetenv("RESTRICTED_IP")
	os.Unsetenv("PROJECT_ID")
	os.Unsetenv("GOOGLE_CLOUD_PROJECT")

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("expected default Port to be 8080, got %s", cfg.Port)
	}
	if cfg.DevMode != false {
		t.Errorf("expected default DevMode to be false, got true")
	}
	if cfg.ProjectID != "git-bucket-79382" {
		t.Errorf("expected default ProjectID to be git-bucket-79382, got %s", cfg.ProjectID)
	}
}
