package kb

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestKbListWiring drives the cobra Execute path (flag -> Session -> commonplace
// -> stdout) against a stub, proving the wiring works offline.
func TestKbListWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hit []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /knowledge/api/knowledge", func(w http.ResponseWriter, r *http.Request) {
		hit = append(hit, r.URL.Path)
		_, _ = w.Write([]byte(`{"entries":[{"id":"e1","topic":"t","visibility":"org"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(hit) == 0 || hit[0] != "/knowledge/api/knowledge" {
		t.Fatalf("knowledge list not hit: %v", hit)
	}
}

// TestStoreContentFromReader checks the content sourcing helper: --content wins,
// else the provided reader (stdin stand-in).
func TestStoreContentFromReader(t *testing.T) {
	got, err := readContent("flag-content", strings.NewReader("ignored"))
	if err != nil || got != "flag-content" {
		t.Fatalf("flag path: %q %v", got, err)
	}
	got, err = readContent("", strings.NewReader("piped body"))
	if err != nil || got != "piped body" {
		t.Fatalf("reader path: %q %v", got, err)
	}
	if _, err := readContent("", strings.NewReader("")); err == nil {
		t.Fatal("empty flag + empty reader should error")
	}
}

func TestKbUpdateWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var body, path string
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /knowledge/api/knowledge/e1", func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"id":"e1","topic":"new","visibility":"org"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"update", "e1", "--topic", "new"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if path != "/knowledge/api/knowledge/e1" || !strings.Contains(body, `"topic":"new"`) || strings.Contains(body, "content") {
		t.Fatalf("path=%q body=%q", path, body)
	}
}

func TestKbUpdateNothing(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /knowledge/api/knowledge/e1", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"update", "e1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected nothing-to-update error")
	}
	if called {
		t.Fatal("update with no flags must not hit the server")
	}
}

func TestKbDeleteWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	hit := false
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /knowledge/api/knowledge/e1", func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"delete", "e1", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !hit {
		t.Fatal("delete endpoint not hit")
	}
}

func TestKbDeleteRequiresYes(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /knowledge/api/knowledge/e1", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"delete", "e1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected --yes-required error")
	}
	if called {
		t.Fatal("delete without --yes must not hit the server")
	}
}
