package auth

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/zalando/go-keyring"
)

func TestStatusJSON(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{CurrentContext: "dev", Contexts: map[string]config.Context{
		"dev":  {Edge: "http://edge:8080", Identity: config.Identity{Kind: "human", Subject: "u1", Display: "alice@x"}},
		"prod": {Edge: "http://prod:8080", Identity: config.Identity{Kind: "agent", Subject: "a1", Display: "builder", Slug: "builder"}},
	}}
	_ = cfg.Save()
	at := "x." + b64(`{"sub":"u1","exp":9999999999}`) + ".y"
	_ = tokenstore.New("http://edge:8080", "dev", "u1").SaveAccess(at, time.Now().Add(time.Hour))

	cmd := newStatusCmd(&GlobalFlags{JSON: true})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var got []statusEntry
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(got) != 2 || got[0].Name != "dev" || got[1].Name != "prod" {
		t.Fatalf("want sorted [dev,prod], got %+v", got)
	}
	if !got[0].Current || got[1].Current {
		t.Fatalf("current flag wrong: %+v", got)
	}
	if got[0].State != "valid" {
		t.Fatalf("dev state = %q, want valid", got[0].State)
	}
	if got[0].Kind != "human" || got[1].Kind != "agent" || got[1].Display != "builder" {
		t.Fatalf("fields: %+v", got)
	}
	if got[0].Subject != "u1" || got[0].Edge != "http://edge:8080" {
		t.Fatalf("dev subject/edge: %+v", got[0])
	}
}

func TestStatusJSONEmpty(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cmd := newStatusCmd(&GlobalFlags{JSON: true})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status --json (empty): %v", err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("empty status --json = %q, want []", out.String())
	}
}

func TestStatusTextSorted(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{CurrentContext: "prod", Contexts: map[string]config.Context{
		"dev":  {Edge: "http://edge:8080", Identity: config.Identity{Kind: "human", Subject: "u1", Display: "alice@x"}},
		"prod": {Edge: "http://prod:8080", Identity: config.Identity{Kind: "agent", Subject: "a1", Display: "builder"}},
	}}
	_ = cfg.Save()
	cmd := newStatusCmd(&GlobalFlags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "dev") || !strings.Contains(lines[1], "prod") {
		t.Fatalf("want sorted dev then prod:\n%s", out.String())
	}
	if !strings.HasPrefix(lines[1], "* ") { // prod is current
		t.Fatalf("prod should have * marker:\n%s", out.String())
	}
}
