package v3fmt

import (
	"crypto/sha256"
	"encoding/binary"
)

// StableID derives a stable, positive int64 identifier from a string key.
// GitHub returns integer IDs while GitBucket uses string Firestore document
// IDs. The mapping is content-addressed (sha256-derived), so the same input
// always renders the same numeric ID across deployments — no storage needed.
// The high bit is masked to keep the value positive (matches GitHub).
func StableID(key string) int64 {
	h := sha256.Sum256([]byte(key))
	v := int64(binary.BigEndian.Uint64(h[:8]))
	// Clear the sign bit so the value is always non-negative.
	// Cast to uint64, mask off bit 63, cast back to int64.
	v = int64(uint64(v) &^ (uint64(1) << 63))
	return v
}
