package issue

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/CarriedWorldUniverse/cw/internal/ledger"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// TestLiveIssue runs the issue work-loop against a real deployment: create → get →
// search → claim → comment. Gated on CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD +
// CW_IT_PROJECT; skips cleanly otherwise so the offline suite stays green.
//
// NOTE: CW_IT_USER must be a working-org identity holding issue:read/write/claim
// (a human created with those scopes) + CW_IT_PROJECT a project key in that org.
// The platform-admin genesis owner (cwadmin) is product-disabled and will NOT
// work — it is firewalled from working-org product data and has no issue:* scopes.
//
//	CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//	CW_IT_USER=worker@example.com CW_IT_PASSWORD=... CW_IT_PROJECT=NEX \
//	go test ./internal/cli/issue/ -run TestLiveIssue -v
func TestLiveIssue(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	project := os.Getenv("CW_IT_PROJECT")
	if edge == "" || project == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD + CW_IT_PROJECT to run the live issue test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())

	c, _ := liveSession(t, edge)
	ctx := context.Background()

	iss, err := ledger.CreateIssue(ctx, c, ledger.CreateInput{
		Project:          project,
		Type:             "Story",
		Summary:          "cw-it " + time.Now().Format(time.RFC3339),
		DefinitionOfDone: "- [x] done",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := ledger.GetIssue(ctx, c, iss.Key); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if _, err := ledger.SearchByProject(ctx, c, project); err != nil {
		t.Fatalf("SearchByProject: %v", err)
	}
	if _, err := ledger.Claim(ctx, c, iss.Key); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := ledger.Comment(ctx, c, iss.Key, "from cw live test"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
}

// liveSession does a password grant against edge (CW_IT_USER/CW_IT_PASSWORD),
// writes an "it" context + caches the access token, and returns a client built by
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
