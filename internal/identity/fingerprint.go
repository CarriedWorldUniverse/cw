package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
)

// Fingerprint is the casket Ed25519 pubkey's stable identifier, matching
// herald's identity.Fingerprint: base64url(sha256(pubkey)[:16]). Deterministic.
// Herald owns this convention (its internal/identity/fingerprint.go); cw mirrors
// it so a locally-derived fingerprint matches herald's stored value + /api/me.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
