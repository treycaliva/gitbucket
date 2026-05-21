package sync

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	kms "cloud.google.com/go/kms/apiv1"
	kmspb "google.golang.org/genproto/googleapis/cloud/kms/v1"
)

// RunGitCommand runs a git command with context cancellation that kills the entire process group.
func RunGitCommand(ctx context.Context, args []string, dir string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Create a new process group for the command so we can kill it and all its children together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start git command: %w", err)
	}

	// Monitor context cancellation in a background goroutine to kill the process group immediately.
	doneChan := make(chan struct{})
	defer close(doneChan)

	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				// Kill the process group (negative PID)
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-doneChan:
		}
	}()

	err := cmd.Wait()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

// CacheEntry represents an entry in the EphemeralCredentialCache.
type CacheEntry struct {
	DecryptedSecret []byte
	ExpiresAt       time.Time
}

// EphemeralCredentialCache provides thread-safe, secure ephemeral in-memory storage for decrypted credentials.
type EphemeralCredentialCache struct {
	mu    sync.RWMutex
	cache map[string]CacheEntry
}

// NewEphemeralCredentialCache instantiates and returns a new EphemeralCredentialCache.
func NewEphemeralCredentialCache(ctx context.Context, cleanupInterval time.Duration) *EphemeralCredentialCache {
	c := &EphemeralCredentialCache{
		cache: make(map[string]CacheEntry),
	}
	// Start background cleanup routine
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.CleanupExpired()
			case <-ctx.Done():
				c.Clear()
				return
			}
		}
	}()
	return c
}

// Get retrieves a decrypted secret byte slice. Callers MUST zero out their copy after use.
func (c *EphemeralCredentialCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, found := c.cache[key]
	if !found {
		return nil, false
	}
	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}

	// Return a copy to avoid race conditions.
	secretCopy := make([]byte, len(entry.DecryptedSecret))
	copy(secretCopy, entry.DecryptedSecret)
	return secretCopy, true
}

// Set saves a decrypted secret in the cache.
func (c *EphemeralCredentialCache) Set(key string, secret []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If there's an existing entry, zero it out first
	if existing, found := c.cache[key]; found {
		for i := range existing.DecryptedSecret {
			existing.DecryptedSecret[i] = 0
		}
	}

	secretCopy := make([]byte, len(secret))
	copy(secretCopy, secret)

	c.cache[key] = CacheEntry{
		DecryptedSecret: secretCopy,
		ExpiresAt:       time.Now().Add(ttl),
	}
}

// Delete removes an entry from the cache, zeroing the decrypted byte slice first.
func (c *EphemeralCredentialCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, found := c.cache[key]; found {
		for i := range entry.DecryptedSecret {
			entry.DecryptedSecret[i] = 0
		}
		delete(c.cache, key)
	}
}

// CleanupExpired scans and clears expired cache entries, zeroing their memory first.
func (c *EphemeralCredentialCache) CleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for k, entry := range c.cache {
		if now.After(entry.ExpiresAt) {
			for i := range entry.DecryptedSecret {
				entry.DecryptedSecret[i] = 0
			}
			delete(c.cache, k)
		}
	}
}

// Clear completely clears the cache, zeroing all stored byte slices.
func (c *EphemeralCredentialCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, entry := range c.cache {
		for i := range entry.DecryptedSecret {
			entry.DecryptedSecret[i] = 0
		}
		delete(c.cache, k)
	}
}

// KMSClient handles Envelope Encryption using Google Cloud KMS.
type KMSClient struct {
	client  *kms.KeyManagementClient
	kekName string
}

// NewKMSClient returns a new KMSClient.
func NewKMSClient(client *kms.KeyManagementClient, kekName string) *KMSClient {
	return &KMSClient{
		client:  client,
		kekName: kekName,
	}
}

// EncryptSecret encrypts a raw secret string using a DEK, which is then wrapped by Cloud KMS.
func (k *KMSClient) EncryptSecret(ctx context.Context, rawSecret string) (encTokenB64, encDEKB64, nonceB64 string, err error) {
	// 1. Generate a random 256-bit DEK
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return "", "", "", err
	}
	defer func() {
		// Zero out temporary DEK
		for i := range dek {
			dek[i] = 0
		}
	}()

	// 2. Encrypt the secret locally using the DEK with AES-GCM
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", "", "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", "", err
	}
	ciphertext := aesGCM.Seal(nil, nonce, []byte(rawSecret), nil)

	// 3. Encrypt the DEK using Cloud KMS (or fallback in dev/local mode)
	var encryptedDEK []byte
	if k == nil || k.client == nil || k.kekName == "" {
		// Fallback: Use the DEK itself as the "encrypted" DEK representation in base64
		encryptedDEK = dek
	} else {
		req := &kmspb.EncryptRequest{
			Name:      k.kekName,
			Plaintext: dek,
		}
		resp, err := k.client.Encrypt(ctx, req)
		if err != nil {
			return "", "", "", err
		}
		encryptedDEK = resp.Ciphertext
	}

	return base64.StdEncoding.EncodeToString(ciphertext),
		base64.StdEncoding.EncodeToString(encryptedDEK),
		base64.StdEncoding.EncodeToString(nonce),
		nil
}

// DecryptSecret unwraps a DEK using Cloud KMS and decrypts a ciphertext base64 token.
func (k *KMSClient) DecryptSecret(ctx context.Context, encTokenB64, encDEKB64, nonceB64 string) ([]byte, error) {
	encryptedToken, err := base64.StdEncoding.DecodeString(encTokenB64)
	if err != nil {
		return nil, err
	}
	encryptedDEK, err := base64.StdEncoding.DecodeString(encDEKB64)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return nil, err
	}

	// 1. Decrypt the DEK via Cloud KMS (or fallback in dev/local mode)
	var dek []byte
	if k == nil || k.client == nil || k.kekName == "" {
		// Fallback: The encrypted DEK is just the raw DEK
		dek = make([]byte, len(encryptedDEK))
		copy(dek, encryptedDEK)
	} else {
		req := &kmspb.DecryptRequest{
			Name:       k.kekName,
			Ciphertext: encryptedDEK,
		}
		resp, err := k.client.Decrypt(ctx, req)
		if err != nil {
			return nil, err
		}
		dek = resp.Plaintext
	}
	defer func() {
		// Zero out the temporary DEK slice
		for i := range dek {
			dek[i] = 0
		}
	}()

	// 2. Decrypt the token locally
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := aesGCM.Open(nil, nonce, encryptedToken, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

var credsCache = NewEphemeralCredentialCache(context.Background(), 1*time.Minute)

// DecryptSecretWithCache checks the memory cache before performing a KMS Decrypt operation.
// It returns a copy of the decrypted secret. Callers MUST zero out their copy after use.
func (k *KMSClient) DecryptSecretWithCache(ctx context.Context, encTokenB64, encDEKB64, nonceB64 string) ([]byte, error) {
	// The ciphertext itself acts as a unique key for caching
	cacheKey := encTokenB64
	if cachedVal, found := credsCache.Get(cacheKey); found {
		return cachedVal, nil
	}

	decrypted, err := k.DecryptSecret(ctx, encTokenB64, encDEKB64, nonceB64)
	if err != nil {
		return nil, err
	}
	defer func() {
		// Zero out local decrypted buffer after caching
		for i := range decrypted {
			decrypted[i] = 0
		}
	}()

	// Ephemerally cache the secret for 5 minutes
	credsCache.Set(cacheKey, decrypted, 5*time.Minute)

	retVal, _ := credsCache.Get(cacheKey)
	return retVal, nil
}
