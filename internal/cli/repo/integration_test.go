package repo

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/cairn"
	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// TestLiveRepoPR exercises cw's cairn wrapper + client + org-resolution against a
// real deployment: password-grant login, then repo create → list. It proves the
// cw-specific value (the live PR-open path needs a ledger project + seeded
// branches, which the conformance journey already covers end-to-end).
//
// Gated on CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD; skips cleanly otherwise so
// the offline suite stays green.
//
//	CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//	CW_IT_USER=cwadmin@carriedworld.com CW_IT_PASSWORD=... \
//	go test ./internal/cli/repo/ -run TestLiveRepoPR -v
func TestLiveRepoPR(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD to run the live repo/pr test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())

	c, ctx := liveSession(t, edge)
	org := ctx.Identity.Org
	if org == "" {
		t.Fatalf("no org in token claims; cannot resolve repo org")
	}
	slug := fmt.Sprintf("cw-it-%d", time.Now().UnixNano())

	if _, err := cairn.CreateRepo(context.Background(), c, org, slug); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	repos, err := cairn.ListRepos(context.Background(), c, org)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	found := false
	for _, r := range repos {
		if r.Slug == slug {
			found = true
		}
	}
	if !found {
		t.Fatalf("created repo %q not in ListRepos", slug)
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
