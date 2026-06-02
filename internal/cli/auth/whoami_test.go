package auth

import (
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/zalando/go-keyring"
)

func TestWhoamiClaims(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	// Seed a context + a non-expired cached access token with known claims.
	cfg := &config.Config{CurrentContext: "dev", Contexts: map[string]config.Context{
		"dev": {Edge: "http://edge:8080", Identity: config.Identity{Kind: "human", Subject: "u1", Display: "alice@x"}},
	}}
	_ = cfg.Save()
	at := "x." + b64(`{"sub":"u1","kind":"human","org":"acme","scope":"issue:read issue:write","products":["cairn","ledger"],"exp":9999999999}`) + ".y"
	_ = tokenstore.New("http://edge:8080", "dev", "u1").SaveAccess(at, time.Now().Add(time.Hour))

	info, err := whoamiInfo(&GlobalFlags{})
	if err != nil {
		t.Fatalf("whoamiInfo: %v", err)
	}
	if info.Subject != "u1" || info.Org != "acme" || info.Kind != "human" {
		t.Fatalf("info: %+v", info)
	}
	if len(info.Scopes) != 2 {
		t.Fatalf("scopes: %v", info.Scopes)
	}
	if len(info.Products) != 2 || info.Products[0] != "cairn" {
		t.Fatalf("products: %v", info.Products)
	}
	if info.ExpiresIn <= 0 {
		t.Fatalf("expires-in should be positive: %d", info.ExpiresIn)
	}
	if info.Edge != "http://edge:8080" {
		t.Fatalf("edge: %q", info.Edge)
	}
	if info.Display != "alice@x" {
		t.Fatalf("display: %q", info.Display)
	}
	if info.Slug != "" {
		t.Fatalf("human should have no slug, got %q", info.Slug)
	}
}

func TestWhoamiAgentSlug(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{CurrentContext: "ag", Contexts: map[string]config.Context{
		"ag": {Edge: "http://edge:8080", Identity: config.Identity{Kind: "agent", Subject: "a1", Display: "builder", Slug: "builder"}},
	}}
	_ = cfg.Save()
	at := "x." + b64(`{"sub":"a1","kind":"agent","org":"acme","scope":"repo:read","products":["cairn"],"exp":9999999999}`) + ".y"
	_ = tokenstore.New("http://edge:8080", "ag", "a1").SaveAccess(at, time.Now().Add(time.Hour))

	info, err := whoamiInfo(&GlobalFlags{})
	if err != nil {
		t.Fatalf("whoamiInfo: %v", err)
	}
	if info.Kind != "agent" || info.Slug != "builder" || info.Display != "builder" {
		t.Fatalf("agent info: %+v", info)
	}
}
