package agent

import (
	"context"
	"os"
	"testing"

	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cwb-client/herald"
	"github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"
	casket "github.com/CarriedWorldUniverse/casket-go"
)

// TestLiveFingerprintMatchesHerald proves cw's local Fingerprint equals herald's
// stored value: log the provisioned agent in, fetch /api/me, and assert its
// fingerprint == identity.Fingerprint(DeriveAgentKey(seed, slug).pub). Guards
// against drift from herald's algorithm.
//
// Gated on CW_IT_EDGE + CW_IT_OWNER_SEED + CW_IT_AGENT_ID + CW_IT_AGENT_SLUG.
func TestLiveFingerprintMatchesHerald(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	seed := os.Getenv("CW_IT_OWNER_SEED")
	agentID := os.Getenv("CW_IT_AGENT_ID")
	slug := os.Getenv("CW_IT_AGENT_SLUG")
	if edge == "" || seed == "" || agentID == "" || slug == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_OWNER_SEED + CW_IT_AGENT_ID + CW_IT_AGENT_SLUG to run the live fingerprint test")
	}
	c, err := liveAgentClient(t, edge, seed, slug, agentID)
	if err != nil {
		t.Fatalf("agent login: %v", err)
	}
	ui, err := herald.Me(context.Background(), c)
	if err != nil {
		t.Fatalf("herald.Me: %v", err)
	}
	_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
	if err != nil {
		t.Fatal(err)
	}
	local := identity.Fingerprint(pub)
	if ui.Fingerprint != local {
		t.Fatalf("herald fingerprint %q != local %q (algorithm drift)", ui.Fingerprint, local)
	}
}

// liveAgentClient logs the agent in via herald's jwt-bearer assertion grant
// (TokenEndpoint -> AgentAssertion -> JWTBearerGrant) and returns a static-token
// client carrying the resulting bearer. Mirrors auth.runLogin's agent path
// (internal/cli/auth/login.go) without touching the token store, so the live
// fingerprint drift-guard test needs no stored context.
func liveAgentClient(t *testing.T, edge, seed, slug, agentID string) (*client.Client, error) {
	t.Helper()
	ctx := context.Background()
	oc := oidc.New(edge)
	tu, err := oc.TokenEndpoint(ctx)
	if err != nil {
		return nil, err
	}
	assertion, err := identity.AgentAssertion([]byte(seed), slug, agentID, tu)
	if err != nil {
		return nil, err
	}
	tok, err := oc.JWTBearerGrant(ctx, assertion)
	if err != nil {
		return nil, err
	}
	return client.WithStaticToken(edge, tok.AccessToken), nil
}
