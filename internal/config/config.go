package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds the application configuration.
type Config struct {
	Port                 string
	GCSBucket            string
	DevMode              bool
	RestrictedIP         string
	ProjectID            string
	LocalReposRoot       string
	LocalReposMaxBytes   int64 // eviction budget for materialized repos; 0 disables
	KMSKeyName           string
	SecretManagerProject string
	// Plan 3: Cloud Tasks webhook engine.
	CloudTasksQueueName    string // e.g. "projects/<p>/locations/us-central1/queues/gitbucket-webhooks"
	DispatcherOIDCSA       string // service account email Cloud Tasks uses for OIDC
	DispatcherOIDCAudience string // expected audience on inbound dispatcher requests
}

// localReposMaxBytes resolves the disk budget for materialized repos under
// LOCAL_REPOS_ROOT. Explicit LOCAL_REPOS_MAX_BYTES wins (0 disables eviction).
// Otherwise, since Cloud Run backs /tmp with tmpfs that counts against the
// instance memory limit, default to half the cgroup v2 memory limit so the
// working set can't OOM the instance; fall back to 2 GiB when the limit is
// unknown or unbounded.
func localReposMaxBytes() int64 {
	if v := os.Getenv("LOCAL_REPOS_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		s := strings.TrimSpace(string(data))
		if s != "max" {
			if limit, err := strconv.ParseInt(s, 10, 64); err == nil && limit > 0 {
				return limit / 2
			}
		}
	}
	return 2 << 30 // 2 GiB
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

	secretManagerProject := os.Getenv("SECRET_MANAGER_PROJECT")
	if secretManagerProject == "" {
		secretManagerProject = projectID
	}

	return &Config{
		Port:                   port,
		GCSBucket:              os.Getenv("GCS_BUCKET"),
		DevMode:                devMode,
		RestrictedIP:           restrictedIP,
		ProjectID:              projectID,
		LocalReposRoot:         localReposRoot,
		LocalReposMaxBytes:     localReposMaxBytes(),
		KMSKeyName:             kmsKeyName,
		SecretManagerProject:   secretManagerProject,
		CloudTasksQueueName:    os.Getenv("CLOUD_TASKS_QUEUE_NAME"),
		DispatcherOIDCSA:       os.Getenv("DISPATCHER_OIDC_SA"),
		DispatcherOIDCAudience: os.Getenv("DISPATCHER_OIDC_AUDIENCE"),
	}
}
