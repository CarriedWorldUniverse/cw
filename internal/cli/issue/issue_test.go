package issue

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestIssueListWiring drives the cobra Execute path (flag -> Session -> ledger ->
// stdout) against a stub, proving the wiring works offline.
func TestIssueListWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hit []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ledger/api/issues/my", func(w http.ResponseWriter, r *http.Request) {
		hit = append(hit, r.URL.Path)
		_, _ = w.Write([]byte(`{"issues":[{"key":"NEX-9","status":"To Do","summary":"S"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"list", "--mine"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(hit) == 0 || hit[0] != "/ledger/api/issues/my" {
		t.Fatalf("ledger /my not hit: %v", hit)
	}
}

func TestListModeMutuallyExclusive(t *testing.T) {
	gf := &cmdutil.GlobalFlags{Edge: "http://x", Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"list", "--mine", "--ready"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "one of") {
		t.Fatalf("want mutually-exclusive error, got %v", err)
	}
}
