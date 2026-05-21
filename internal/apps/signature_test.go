package apps

import (
	"strings"
	"testing"
)

func TestComputeHubSignature(t *testing.T) {
	// Known-good test vector: payload "hello", secret "topsecret".
	// HMAC-SHA256 hex computed via:
	//   echo -n 'hello' | openssl dgst -sha256 -hmac 'topsecret' -hex
	want := "sha256=ed76fd36523b8becda5a3b36d0e3737e8ae5111f55e26c7c3a455a3ce29636d2"
	got := ComputeHubSignature([]byte("hello"), []byte("topsecret"))
	if !strings.EqualFold(got, want) {
		t.Errorf("ComputeHubSignature = %q, want %q", got, want)
	}
}

func TestVerifyHubSignature(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte("s3cret")
	sig := ComputeHubSignature(payload, secret)

	if !VerifyHubSignature(payload, secret, sig) {
		t.Error("VerifyHubSignature rejected a signature we just computed")
	}
	if VerifyHubSignature(payload, []byte("wrong-secret"), sig) {
		t.Error("VerifyHubSignature accepted a signature from the wrong secret")
	}
	if VerifyHubSignature([]byte("tampered"), secret, sig) {
		t.Error("VerifyHubSignature accepted a signature for tampered payload")
	}
}

func TestVerifyHubSignatureMissingPrefix(t *testing.T) {
	if VerifyHubSignature([]byte("x"), []byte("s"), "not-a-sig") {
		t.Error("expected false for malformed signature")
	}
}
