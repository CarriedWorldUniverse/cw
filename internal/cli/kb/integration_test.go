package kb

import (
	"context"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cwb-client/commonplace"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"os"
)

// TestLiveKB stores an entry then proves SEMANTIC retrieval: a query worded
// differently from the stored content surfaces it (commonplace embeds via ollama
// on dMon). Gated on CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD; skips otherwise so
// the offline suite stays green.
//
// NOTE: CW_IT_USER must be a working-org identity holding knowledge:read/write.
// The platform-admin genesis owner (cwadmin) is product-disabled and will NOT
// work — it is firewalled from working-org product data and has no knowledge:*
// scopes.
//
//	CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//	CW_IT_USER=worker@example.com CW_IT_PASSWORD=... \
//	go test ./internal/cli/kb/ -run TestLiveKB -v
func TestLiveKB(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD to run the live kb test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())

	c, _ := liveSession(t, edge)
	ctx := context.Background()

	marker := "cwkb-" + time.Now().Format("150405")
	stored, err := commonplace.Store(ctx, c, commonplace.StoreInput{
		Topic:      marker,
		Content:    "The CWB platform is deployed on a single-node k3s cluster on the dMon machine. " + marker,
		Visibility: "org",
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	// Differently-worded query (shares few tokens with the content) must surface it.
	hits, err := commonplace.Search(ctx, c, "where is the platform hosted", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.Entry.ID == stored.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("semantic search did not surface stored entry %s (hits=%d)", stored.ID, len(hits))
	}
	if _, err := commonplace.List(ctx, c); err != nil {
		t.Fatalf("List: %v", err)
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
