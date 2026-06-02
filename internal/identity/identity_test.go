package identity

import (
	"encoding/base64"
	"testing"

	casket "github.com/CarriedWorldUniverse/casket-go"
	jose "github.com/go-jose/go-jose/v4"
)

func TestAgentAssertionVerifies(t *testing.T) {
	seed := []byte("owner-seed-32-bytes-padded-xxxxx")
	_, pub, err := casket.DeriveAgentKey(seed, "shadow")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	assertion, err := AgentAssertion(seed, "shadow", "agent-123", "http://edge:8080/herald/token")
	if err != nil {
		t.Fatalf("AgentAssertion: %v", err)
	}
	// Verify the assertion against the derived public key (what herald does).
	jws, err := jose.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	payload, err := jws.Verify(pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	claims := DecodeClaimsBytes(payload)
	if claims["iss"] != "agent-123" || claims["sub"] != "agent-123" || claims["aud"] != "http://edge:8080/herald/token" {
		t.Fatalf("claims: %+v", claims)
	}
}

func TestDecodeAccessClaims(t *testing.T) {
	// header.payload.sig where payload = base64url({"sub":"u1","kind":"human","scope":"a b"})
	tok := "x." + b64url(`{"sub":"u1","kind":"human","scope":"a b","exp":111}`) + ".y"
	claims, err := DecodeAccessClaims(tok)
	if err != nil || claims["sub"] != "u1" || claims["kind"] != "human" {
		t.Fatalf("DecodeAccessClaims: %v %+v", err, claims)
	}
}

func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
