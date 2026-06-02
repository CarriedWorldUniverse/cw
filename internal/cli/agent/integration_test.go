package agent

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	casket "github.com/CarriedWorldUniverse/casket-go"
)

// TestLiveAgent provisions an agent end to end against live herald and proves
// the create->derive->register->login loop: as platform-admin, create a
// cwb-test-* org + responsible human, derive an agent key from a fresh seed,
// register it, then log the agent IN via the jwt-bearer assertion grant and
// assert the token carries the granted scope and kind=agent.
//
// Gated on CW_IT_EDGE + CW_IT_USER (platform-admin) + CW_IT_PASSWORD; skips otherwise.
// CW_IT_USER MUST be the platform-admin (genesis owner, e.g.
// cwadmin@carriedworld.com) — a working-org human gets 403 on org create.
// The cwb-test-* org name lets the conformance reaper collect it.
//
//	CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//	CW_IT_USER=cwadmin@carriedworld.com CW_IT_PASSWORD=... \
//	go test ./internal/cli/agent/ -run TestLiveAgent -v
func TestLiveAgent(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER (platform-admin) + CW_IT_PASSWORD to run the live agent test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	c, _ := liveSession(t, edge) // platform-admin client

	ctx := context.Background()
	marker := "cwb-test-agent-" + time.Now().Format("150405")
	org, err := herald.CreateOrg(ctx, c, herald.CreateOrgInput{Name: marker})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	human, err := herald.CreateHuman(ctx, c, org.ID, herald.CreateHumanInput{DisplayName: "owner-" + marker})
	if err != nil {
		t.Fatalf("CreateHuman: %v", err)
	}

	seed := "agent-owner-seed-" + marker // any string is a valid HKDF seed; varies per run
	const slug = "builder"
	_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := herald.CreateAgent(ctx, c, org.ID, herald.CreateAgentInput{
		DisplayName:      slug,
		ResponsibleHuman: human.ID,
		CasketPubkey:     base64.StdEncoding.EncodeToString(pub),
		Scopes:           []string{"repo:read"},
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	// Log the agent in: derive the SAME key from (seed, slug), sign an assertion,
	// exchange it for a token. Proves the registered pubkey matches the key.
	// Mirrors auth.runLogin's agent path (TokenEndpoint -> AgentAssertion ->
	// JWTBearerGrant).
	oc := oidc.New(edge)
	tu, err := oc.TokenEndpoint(ctx)
	if err != nil {
		t.Fatalf("token endpoint: %v", err)
	}
	assertion, err := identity.AgentAssertion([]byte(seed), slug, ag.ID, tu)
	if err != nil {
		t.Fatalf("assertion: %v", err)
	}
	tok, err := oc.JWTBearerGrant(ctx, assertion)
	if err != nil {
		t.Fatalf("agent login (jwt-bearer): %v", err)
	}
	claims, err := identity.DecodeAccessClaims(tok.AccessToken)
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if kind, _ := claims["kind"].(string); kind != "agent" {
		t.Fatalf("token kind = %q, want agent", kind)
	}
	if scope, _ := claims["scope"].(string); !strings.Contains(scope, "repo:read") {
		t.Fatalf("agent token missing repo:read; scope=%q", scope)
	}
}

// liveSession does a password grant against edge (CW_IT_USER/CW_IT_PASSWORD),
// writes a "it" context + caches the access token, and returns a client built by
// cmdutil.Session plus the resolved context. It mirrors the minimal token+context
// setup of auth.runLogin so there is no import cycle with the auth package.
func liveSession(t *testing.T, edge string) (*client.Client, config.Context) {
	t.Helper()
	const name = "it"

	tok, err := oidc.New(edge).PasswordGrant(context.Background(), os.Getenv("CW_IT_USER"), os.Getenv("CW_IT_PASSWORD"))
	if err != nil {
		t.Fatalf("password grant: %v", err)
	}

	// Claims are unverified (display + org + keychain-key only).
	claims, _ := identity.DecodeAccessClaims(tok.AccessToken)
	subject, _ := claims["sub"].(string)
	if subject == "" {
		subject = os.Getenv("CW_IT_USER")
	}
	org, _ := claims["org"].(string)

	store := tokenstore.New(edge, name, subject)
	if err := store.SaveRefresh(tok.RefreshToken); err != nil {
		t.Fatalf("save refresh: %v", err)
	}
	exp := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if err := store.SaveAccess(tok.AccessToken, exp); err != nil {
		t.Fatalf("save access: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	cfg.Upsert(name, config.Context{
		Edge:     edge,
		Identity: config.Identity{Kind: "human", Subject: subject, Display: os.Getenv("CW_IT_USER"), Org: org},
	})
	cfg.CurrentContext = name
	if err := cfg.Save(); err != nil {
		t.Fatalf("config save: %v", err)
	}

	c, ctx, _, err := cmdutil.Session(&cmdutil.GlobalFlags{Context: name})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	return c, ctx
}
