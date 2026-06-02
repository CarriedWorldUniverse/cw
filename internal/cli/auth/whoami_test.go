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
		"dev": {Edge: "http://edge:8080", Identity: config.Identity{Kind: "human", Subject: "u1"}},
	}}
	_ = cfg.Save()
	at := "x." + b64(`{"sub":"u1","kind":"human","org":"acme","scope":"issue:read issue:write","exp":9999999999}`) + ".y"
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
}
