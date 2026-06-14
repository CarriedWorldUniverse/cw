package auth

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// stubHeraldIdentity serves discovery (with the agent_auth block) + the
// /agent/identity endpoint, recording whether identity was actually hit and
// echoing the requested audience into the minted token.
func stubHeraldIdentity(t *testing.T, hit *int32) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_endpoint":"` + srv.URL + `/herald/token","agent_auth":{"identity_endpoint":"` + srv.URL + `/herald/agent/identity"}}`))
	})
	mux.HandleFunc("POST /herald/agent/identity", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hit, 1)
		_ = r.ParseForm()
		if r.Form.Get("type") != "identity_assertion" || r.Form.Get("assertion") == "" || r.Form.Get("audience") == "" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"idjag-for-` + r.Form.Get("audience") + `","issued_token_type":"urn:ietf:params:oauth:token-type:id-jag","expires_in":300}`))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestIdJAGCmd_MintsAndPrints(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_OWNER_SEED", "owner-seed-32-bytes-padded-xxxxx")
	var hit int32
	srv := stubHeraldIdentity(t, &hit)

	gf := &GlobalFlags{}
	root := newTestRoot(gf)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{
		"auth", "idjag",
		"--agent-id", "agent-123", "--slug", "shadow",
		"--audience", "ledger", "--edge", srv.URL,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if atomic.LoadInt32(&hit) == 0 {
		t.Fatal("/agent/identity never hit")
	}
	if got := strings.TrimSpace(out.String()); got != "idjag-for-ledger" {
		t.Fatalf("stdout = %q, want the minted ID-JAG", got)
	}
}

func TestIdJAGCmd_RequiresAudience(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_OWNER_SEED", "owner-seed-32-bytes-padded-xxxxx")
	gf := &GlobalFlags{}
	root := newTestRoot(gf)
	root.SetArgs([]string{
		"auth", "idjag",
		"--agent-id", "agent-123", "--slug", "shadow",
		"--edge", "http://unused",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("missing --audience must error")
	}
}

func TestIdJAGCmd_RequiresSeed(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_OWNER_SEED", "")
	gf := &GlobalFlags{}
	root := newTestRoot(gf)
	root.SetArgs([]string{
		"auth", "idjag",
		"--agent-id", "agent-123", "--slug", "shadow",
		"--audience", "ledger", "--edge", "http://unused",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("missing CW_OWNER_SEED must error")
	}
}
