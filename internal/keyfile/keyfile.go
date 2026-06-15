// Package keyfile builds the herald-rooted bootstrap keyfile the nexus aspect
// runtime reads (nexus runtime/heraldkeyfile.Keyfile). It is the shared wire
// contract for `cw agent enroll` and `cw tenant onboard` (croft provisioning):
// both derive a casket key from an owner seed + slug and pair it with the
// herald-assigned agent id.
package keyfile

import "encoding/base64"

// Bootstrap is the aspect bootstrap keyfile. JSON tags MUST match nexus
// runtime/heraldkeyfile.Keyfile — do not rename without changing both.
type Bootstrap struct {
	Key         string `json:"key"`         // base64(std) ed25519 private key (64-byte Go form)
	KeyID       string `json:"key_id"`      // herald agent UUID
	URL         string `json:"url"`         // nexus relay/seam the aspect connects/discovers through
	Slug        string `json:"slug"`        // agent name (casket key slug)
	Fingerprint string `json:"fingerprint"` // base64url sha256(pub)[:16]
}

// Build assembles a Bootstrap from the derived private key + the resolved herald
// identity. priv is the raw ed25519 private key (it is base64-encoded here).
func Build(priv []byte, agentID, url, slug, fingerprint string) Bootstrap {
	return Bootstrap{
		Key:         base64.StdEncoding.EncodeToString(priv),
		KeyID:       agentID,
		URL:         url,
		Slug:        slug,
		Fingerprint: fingerprint,
	}
}
