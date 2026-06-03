package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestWhoamiTopLevelFactory(t *testing.T) {
	// The exported factory builds a `whoami` command (used both at root and
	// under `cw auth`).
	cmd := NewWhoamiCmd(&GlobalFlags{})
	if cmd.Use != "whoami" {
		t.Fatalf("top-level factory Use = %q, want whoami", cmd.Use)
	}
	// And `cw auth` still has a whoami subcommand (the alias).
	var found bool
	for _, sub := range NewCmd(&GlobalFlags{}).Commands() {
		if sub.Name() == "whoami" {
			found = true
		}
	}
	if !found {
		t.Fatal("cw auth is missing its whoami subcommand")
	}
}

func TestWhoamiRemote(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hit string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, r *http.Request) {
		hit = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"a1","kind":"agent","display_name":"builder","org":"o1","org_name":"acme","status":"active","scopes":["repo:read","repo:write"],"responsible_human":"h1","fingerprint":"SHA256:zzz"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewWhoamiCmd(gf)
	cmd.SetArgs([]string{"--remote"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("whoami --remote: %v", err)
	}
	if hit != "/herald/api/me" {
		t.Fatalf("endpoint not hit: %q", hit)
	}
	s := out.String()
	for _, want := range []string{"agent", "builder", "acme", "active", "repo:write", "h1", "SHA256:zzz"} {
		if !strings.Contains(s, want) {
			t.Fatalf("--remote output missing %q:\n%s", want, s)
		}
	}
}

func TestWhoamiRemoteJSON(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"h1","kind":"human","display_name":"alice@x","org":"o1","org_name":"acme","status":"active","scopes":["issue:read"]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &GlobalFlags{Edge: srv.URL, Token: "tok", JSON: true}
	cmd := NewWhoamiCmd(gf)
	cmd.SetArgs([]string{"--remote"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("whoami --remote --json: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if got["id"] != "h1" || got["org_name"] != "acme" {
		t.Fatalf("unexpected --json: %v", got)
	}
}
