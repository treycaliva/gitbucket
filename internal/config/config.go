package config

import (
	"os"
	"strconv"
)

// Config holds the application configuration.
type Config struct {
	Port           string
	GCSBucket      string
	DevMode        bool
	RestrictedIP   string
	ProjectID      string
	LocalReposRoot string
	KMSKeyName     string
}

// Load loads the configuration from environment variables.
func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	devMode := false
	if dm := os.Getenv("DEV_MODE"); dm != "" {
		if b, err := strconv.ParseBool(dm); err == nil {
			devMode = b
		}
	}

	restrictedIP := os.Getenv("RESTRICTED_IP")

	projectID := os.Getenv("PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		projectID = "git-bucket-79382"
	}

	localReposRoot := os.Getenv("LOCAL_REPOS_ROOT")
	if localReposRoot == "" {
		localReposRoot = "/tmp/repos"
	}

	kmsKeyName := os.Getenv("KMS_KEY_NAME")
	if kmsKeyName == "" {
		kmsKeyName = os.Getenv("KMS_KEK_NAME")
	}

	return &Config{
		Port:           port,
		GCSBucket:      os.Getenv("GCS_BUCKET"),
		DevMode:        devMode,
		RestrictedIP:   restrictedIP,
		ProjectID:      projectID,
		LocalReposRoot: localReposRoot,
		KMSKeyName:     kmsKeyName,
	}
}
