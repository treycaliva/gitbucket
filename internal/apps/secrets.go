package apps

import (
	"context"
	"fmt"
	"sync"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// SecretStore abstracts secret persistence so production can use Secret Manager
// and dev / tests can use an in-memory store. Resource names are opaque strings
// from the store's perspective — for Secret Manager they look like
// `projects/<p>/secrets/<id>/versions/latest`; for memory store they are local
// keys.
type SecretStore interface {
	// Put stores plaintext under a logical name (e.g. "apps/{app_id}/private-key")
	// and returns the resource name to persist on the App row.
	Put(ctx context.Context, name string, plaintext []byte) (resourceName string, err error)
	// Get fetches the plaintext for a previously-stored resource name.
	Get(ctx context.Context, resourceName string) ([]byte, error)
	// Delete removes a secret. Returning nil on already-absent is acceptable.
	Delete(ctx context.Context, resourceName string) error
}

// --- Memory implementation -------------------------------------------------

type MemorySecretStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{m: make(map[string][]byte)}
}

func (s *MemorySecretStore) Put(_ context.Context, name string, plaintext []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(plaintext))
	copy(cp, plaintext)
	s.m[name] = cp
	return "memory://" + name, nil
}

func (s *MemorySecretStore) Get(_ context.Context, resourceName string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := resourceName
	if len(resourceName) > len("memory://") && resourceName[:len("memory://")] == "memory://" {
		key = resourceName[len("memory://"):]
	}
	v, ok := s.m[key]
	if !ok {
		return nil, fmt.Errorf("secret not found: %s", resourceName)
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (s *MemorySecretStore) Delete(_ context.Context, resourceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceName
	if len(resourceName) > len("memory://") && resourceName[:len("memory://")] == "memory://" {
		key = resourceName[len("memory://"):]
	}
	delete(s.m, key)
	return nil
}

// --- Secret Manager implementation -----------------------------------------

type RealSecretStore struct {
	client    *secretmanager.Client
	projectID string
}

func NewRealSecretStore(client *secretmanager.Client, projectID string) *RealSecretStore {
	return &RealSecretStore{client: client, projectID: projectID}
}

func (s *RealSecretStore) Put(ctx context.Context, name string, plaintext []byte) (string, error) {
	secretID := sanitizeSecretID(name)
	parent := fmt.Sprintf("projects/%s", s.projectID)

	// Create the Secret container if missing; tolerate AlreadyExists.
	_, err := s.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   parent,
		SecretId: secretID,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
		},
	})
	if err != nil && !isAlreadyExists(err) {
		return "", fmt.Errorf("create secret %s: %w", secretID, err)
	}

	// Add a new version with the plaintext.
	versionResp, err := s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  fmt.Sprintf("%s/secrets/%s", parent, secretID),
		Payload: &secretmanagerpb.SecretPayload{Data: plaintext},
	})
	if err != nil {
		return "", fmt.Errorf("add secret version: %w", err)
	}
	return versionResp.Name, nil
}

func (s *RealSecretStore) Get(ctx context.Context, resourceName string) ([]byte, error) {
	resp, err := s.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: resourceName,
	})
	if err != nil {
		return nil, fmt.Errorf("access secret %s: %w", resourceName, err)
	}
	return resp.Payload.Data, nil
}

func (s *RealSecretStore) Delete(ctx context.Context, resourceName string) error {
	// resourceName for a version is .../secrets/<id>/versions/<n>. To delete the
	// whole secret we trim back to .../secrets/<id>.
	parent := trimVersion(resourceName)
	err := s.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: parent})
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

func sanitizeSecretID(name string) string {
	// Secret Manager allows [A-Za-z0-9_-]; replace slashes from logical names.
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

func trimVersion(s string) string {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == '/' {
			// Look for "/versions/" boundary.
			if i >= len("/versions/") && s[i-len("/versions/")+1:i+1] == "/versions/" {
				return s[:i-len("/versions/")+1]
			}
		}
	}
	return s
}

func isAlreadyExists(err error) bool {
	return err != nil && (containsCode(err, "AlreadyExists") || containsCode(err, "already exists"))
}
func isNotFound(err error) bool {
	return err != nil && (containsCode(err, "NotFound") || containsCode(err, "not found"))
}
func containsCode(err error, needle string) bool {
	s := err.Error()
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
