package org

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestOrgListWiring drives cobra Execute (flag -> Session -> herald -> stdout)
// against a stub, proving the wiring works offline.
func TestOrgListWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hit []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/orgs", func(w http.ResponseWriter, r *http.Request) {
		hit = append(hit, r.URL.Path)
		_, _ = w.Write([]byte(`{"orgs":[{"id":"o1","name":"acme"}]}`))
	})
	mux.HandleFunc("GET /herald/api/orgs/o1/products", func(w http.ResponseWriter, r *http.Request) {
		hit = append(hit, r.URL.Path)
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":false}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	for _, args := range [][]string{{"list"}, {"products", "o1"}} {
		cmd := NewCmd(gf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
	}
	if len(hit) != 2 || hit[0] != "/herald/api/orgs" || hit[1] != "/herald/api/orgs/o1/products" {
		t.Fatalf("endpoints hit = %v", hit)
	}
}

// TestDeleteRequiresConfirm: delete fails fast (no HTTP call) when --confirm is
// omitted. herald does the authoritative name-equality check on the happy path.
func TestDeleteRequiresConfirm(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /herald/api/orgs/o1", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"delete", "o1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing-confirm error")
	}
	if called {
		t.Fatal("delete must not hit the server when --confirm is omitted")
	}
}

// TestProductToggleWiring proves enable hits .../enable and disable hits
// .../disable (the shared toggle command dispatches to the right herald func).
func TestProductToggleWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hits []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/products/cairn/enable", func(w http.ResponseWriter, _ *http.Request) {
		hits = append(hits, "enable")
		_, _ = w.Write([]byte(`{"cairn":true}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/products/cairn/disable", func(w http.ResponseWriter, _ *http.Request) {
		hits = append(hits, "disable")
		_, _ = w.Write([]byte(`{"cairn":false}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	for _, args := range [][]string{{"enable", "o1", "cairn"}, {"disable", "o1", "cairn"}} {
		cmd := NewCmd(gf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
	}
	if len(hits) != 2 || hits[0] != "enable" || hits[1] != "disable" {
		t.Fatalf("endpoint routing = %v", hits)
	}
}
