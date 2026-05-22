package apps

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ComputeHubSignature returns the value to put in the X-Hub-Signature-256
// HTTP header. Format: "sha256=" + lowercase hex of HMAC-SHA256(secret, payload).
func ComputeHubSignature(payload, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyHubSignature checks whether `signature` matches the expected value
// for the given payload + secret. The comparison is constant-time to defend
// against timing attacks.
func VerifyHubSignature(payload, secret []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	expected := ComputeHubSignature(payload, secret)
	return hmac.Equal([]byte(expected), []byte(signature))
}
