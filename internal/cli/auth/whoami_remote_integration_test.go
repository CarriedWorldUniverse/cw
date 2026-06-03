package auth

import (
	"context"
	"os"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
)

// TestLiveWhoamiRemote logs in a provisioned agent and asserts herald.Me returns
// the authoritative agent record (kind=agent, responsible_human + fingerprint,
// status=active). Gated on CW_IT_EDGE + CW_IT_AGENT_* (set by the controller's
// live run, which provisions an agent via cw and exports its login material).
//
// Required env: CW_IT_EDGE, CW_IT_OWNER_SEED, CW_IT_AGENT_ID, CW_IT_AGENT_SLUG.
//
//	CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//	CW_IT_OWNER_SEED=... CW_IT_AGENT_ID=... CW_IT_AGENT_SLUG=builder \
//	go test ./internal/cli/auth/ -run TestLiveWhoamiRemote -v
func TestLiveWhoamiRemote(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" || os.Getenv("CW_IT_AGENT_ID") == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_OWNER_SEED + CW_IT_AGENT_ID + CW_IT_AGENT_SLUG to run the live remote-whoami test")
	}
	ctx := context.Background()
	// Log the agent in via the same assertion path cw auth login --agent uses.
	c, err := liveAgentClient(t, edge,
		os.Getenv("CW_IT_OWNER_SEED"), os.Getenv("CW_IT_AGENT_SLUG"), os.Getenv("CW_IT_AGENT_ID"))
	if err != nil {
		t.Fatalf("agent login: %v", err)
	}
	ui, err := herald.Me(ctx, c)
	if err != nil {
		t.Fatalf("herald.Me: %v", err)
	}
	if ui.Kind != "agent" || ui.ResponsibleHuman == "" || ui.Fingerprint == "" || ui.Status != "active" {
		t.Fatalf("agent UserInfo: %+v", ui)
	}
}

// liveAgentClient logs the agent in via herald's jwt-bearer assertion grant
// (TokenEndpoint -> AgentAssertion -> JWTBearerGrant) and returns a static-token
// client carrying the resulting bearer. Mirrors auth.runLogin's agent path
// (internal/cli/auth/login.go) without touching the token store, so the live
// remote-whoami test needs no stored context.
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
