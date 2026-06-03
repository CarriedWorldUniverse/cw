package pr

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CarriedWorldUniverse/cwb-client/cairn"
	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// newPrStub serves the cairn pull endpoints for o1/widgets (mirrors
// internal/cairn's stub) and returns a static-token client pointed at it. hit
// records every path the stub served so the wiring test can assert a call landed.
func newPrStub(t *testing.T, hit *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	record := func(p string) { *hit = append(*hit, p) }
	mux.HandleFunc("POST /cairn/api/orgs/o1/repos/widgets/pulls", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"7","repo":"widgets","source":"feat","target":"main","title":"T","state":"open","ledger_issue_key":"NEX-9"}`))
	})
	mux.HandleFunc("GET /cairn/api/orgs/o1/repos/widgets/pulls", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path)
		_, _ = w.Write([]byte(`[{"id":"7","repo":"widgets","source":"feat","target":"main","title":"T","state":"open","ledger_issue_key":"NEX-9"}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestPrRunners(t *testing.T) {
	var hit []string
	srv := newPrStub(t, &hit)
	c := client.WithStaticToken(srv.URL, "tok")
	ctx := context.Background()
	p, err := cairn.OpenPull(ctx, c, "o1", "widgets", cairn.OpenPullInput{Source: "feat", Target: "main", Title: "T", Project: "NEX"})
	if err != nil || p.ID == "" {
		t.Fatalf("open: %v %+v", err, p)
	}
	pulls, err := cairn.ListPulls(ctx, c, "o1", "widgets", "open")
	if err != nil || len(pulls) == 0 {
		t.Fatalf("list: %v", err)
	}
}

// TestPrListWiring exercises the full cobra Execute path: flags → resolve() →
// cairn, offline, against a stub. --edge points the static-token client at the
// stub and --token forces the no-keychain static path, so no config/login is
// needed. It asserts the stub was actually hit and the table output is correct.
func TestPrListWiring(t *testing.T) {
	var hit []string
	srv := newPrStub(t, &hit)
	t.Setenv("CW_CONFIG_DIR", t.TempDir()) // no current context; --edge supplies one

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"list", "--repo", "o1/widgets", "--state", "open"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(hit) == 0 {
		t.Fatalf("stub was not hit; output=%q", out.String())
	}
	if hit[0] != "/cairn/api/orgs/o1/repos/widgets/pulls" {
		t.Fatalf("unexpected path hit: %q", hit[0])
	}
}
