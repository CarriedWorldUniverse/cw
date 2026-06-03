package identity

import (
	"encoding/base64"
	"testing"

	casket "github.com/CarriedWorldUniverse/casket-go"
)

func TestFingerprint(t *testing.T) {
	_, pub, err := casket.DeriveAgentKey([]byte("cw-pubkey-test-seed"), "builder")
	if err != nil {
		t.Fatal(err)
	}
	fp := Fingerprint(pub)
	// Pinned value (must match herald's base64url(sha256(pub)[:16])).
	if fp != "vhkj2Fplk7uTkGzGSKDEJQ" {
		t.Fatalf("fingerprint = %q, want vhkj2Fplk7uTkGzGSKDEJQ", fp)
	}
	// Format: base64url (no padding) of 16 bytes = 22 chars.
	if len(fp) != 22 {
		t.Fatalf("fingerprint length = %d, want 22", len(fp))
	}
	raw, err := base64.RawURLEncoding.DecodeString(fp)
	if err != nil || len(raw) != 16 {
		t.Fatalf("fingerprint not 16-byte base64url: %v (%d bytes)", err, len(raw))
	}
	// Deterministic.
	if Fingerprint(pub) != fp {
		t.Fatal("Fingerprint not deterministic")
	}
}
