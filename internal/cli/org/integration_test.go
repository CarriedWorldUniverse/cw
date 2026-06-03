package org

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cwb-client/herald"
	"github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// TestLiveAdmin provisions end to end against live herald: create a cwb-test-*
// org, flip a product off then on (assert the map changes), create a human with
// knowledge scopes, set its password, and log THAT human in — proving the
// provisioned identity works and carries the granted scopes.
//
// Gated on CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD; skips otherwise.
// CW_IT_USER MUST be the platform-admin (genesis owner, e.g.
// cwadmin@carriedworld.com) — a working-org human gets 403 on org create.
// The cwb-test-* org name lets the conformance reaper collect it.
//
//	CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//	CW_IT_USER=cwadmin@carriedworld.com CW_IT_PASSWORD=... \
//	go test ./internal/cli/org/ -run TestLiveAdmin -v
func TestLiveAdmin(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER (platform-admin) + CW_IT_PASSWORD to run the live admin test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	c, _ := liveSession(t, edge) // platform-admin client

	ctx := context.Background()
	marker := "cwb-test-org-" + time.Now().Format("150405")
	org, err := herald.CreateOrg(ctx, c, herald.CreateOrgInput{Name: marker})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	t.Logf("created org %s (%s)", org.ID, org.Name)

	off, err := herald.DisableProduct(ctx, c, org.ID, "ledger")
	if err != nil || off["ledger"] != false {
		t.Fatalf("DisableProduct: %v %+v", err, off)
	}
	on, err := herald.EnableProduct(ctx, c, org.ID, "ledger")
	if err != nil || on["ledger"] != true {
		t.Fatalf("EnableProduct: %v %+v", err, on)
	}

	h, err := herald.CreateHuman(ctx, c, org.ID, herald.CreateHumanInput{
		DisplayName: "kb-user-" + marker,
		Scopes:      []string{"knowledge:read", "knowledge:write"},
	})
	if err != nil {
		t.Fatalf("CreateHuman: %v", err)
	}
	pw := "provisioned-pw-" + marker
	if err := herald.SetHumanPassword(ctx, c, h.ID, pw); err != nil {
		t.Fatalf("SetHumanPassword: %v", err)
	}

	// Log the provisioned human in (username = its herald id) and assert the
	// minted token carries the knowledge scopes.
	tok, err := oidc.New(edge).PasswordGrant(ctx, h.ID, pw)
	if err != nil {
		t.Fatalf("login provisioned human: %v", err)
	}
	claims, err := identity.DecodeAccessClaims(tok.AccessToken)
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	scope, _ := claims["scope"].(string) // DecodeAccessClaims returns map[string]any
	if !strings.Contains(scope, "knowledge:write") {
		t.Fatalf("provisioned token missing knowledge:write; scope=%q", scope)
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
