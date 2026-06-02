package config

import (
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CW_CONFIG_DIR", dir)

	// Missing file -> empty config, no error.
	c, err := Load()
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if len(c.Contexts) != 0 || c.CurrentContext != "" {
		t.Fatalf("fresh config not empty: %+v", c)
	}

	c.Upsert("dev", Context{Edge: "http://edge:8080", Identity: Identity{Kind: "human", Subject: "u1", Display: "a@x"}})
	c.CurrentContext = "dev"
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cur, ok := got.Current()
	if !ok || cur.Edge != "http://edge:8080" || cur.Identity.Subject != "u1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if filepath.Dir(path()) != dir {
		t.Fatalf("path() = %q, want under %q", path(), dir)
	}
}

func TestResolvePrecedence(t *testing.T) {
	c := &Config{CurrentContext: "dev", Contexts: map[string]Context{
		"dev":  {Edge: "http://dev:8080"},
		"prod": {Edge: "http://prod:8080"},
	}}
	// Explicit name wins.
	ctx, name, err := c.Resolve("prod", "")
	if err != nil || name != "prod" || ctx.Edge != "http://prod:8080" {
		t.Fatalf("explicit name: %v %q %+v", err, name, ctx)
	}
	// Name + edge: the named context wins, edge is overridden on it.
	ctx, name, err = c.Resolve("prod", "http://override:9000")
	if err != nil || name != "prod" || ctx.Edge != "http://override:9000" {
		t.Fatalf("name+edge: %v %q %+v", err, name, ctx)
	}
	// Unknown name -> error.
	if _, _, err := c.Resolve("nope", ""); err == nil {
		t.Fatal("unknown context should error")
	}
	// --edge override with no name -> ephemeral context off current.
	ctx, _, err = c.Resolve("", "http://override:9000")
	if err != nil || ctx.Edge != "http://override:9000" {
		t.Fatalf("edge override: %v %+v", err, ctx)
	}
	// Default -> current.
	ctx, name, _ = c.Resolve("", "")
	if name != "dev" || ctx.Edge != "http://dev:8080" {
		t.Fatalf("default: %q %+v", name, ctx)
	}
}
