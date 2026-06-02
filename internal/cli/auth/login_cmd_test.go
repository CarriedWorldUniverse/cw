package auth

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
)

// newTestRoot mirrors cmd/cw/main.go's wiring: it binds the persistent flags
// directly onto gf's fields, so cobra populates the same struct the auth
// subcommands read inside RunE. This is the flag-timing contract under test.
func newTestRoot(gf *GlobalFlags) *cobra.Command {
	root := &cobra.Command{Use: "cw", SilenceUsage: true, SilenceErrors: true}
	p := root.PersistentFlags()
	p.StringVar(&gf.Context, "context", "", "context name")
	p.StringVar(&gf.Edge, "edge", "", "edge URL")
	p.StringVar(&gf.Token, "token", "", "bearer token")
	p.StringVar(&gf.Identity, "identity", "", "identity file")
	p.BoolVar(&gf.JSON, "json", false, "json output")
	root.AddCommand(NewCmd(gf))
	return root
}

// stubHeraldAgent serves discovery + a jwt-bearer token endpoint, recording
// whether the token endpoint was actually hit.
func stubHeraldAgent(t *testing.T, hit *int32) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_endpoint":"` + srv.URL + `/herald/token"}`))
	})
	mux.HandleFunc("POST /herald/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hit, 1)
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			at := "x." + b64(`{"sub":"agent-123","kind":"agent","exp":9999999999}`) + ".y"
			_, _ = w.Write([]byte(`{"access_token":"` + at + `","expires_in":600,"refresh_token":"r-1"}`))
			return
		}
		w.WriteHeader(401)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestLoginCmdFlagsReachRunLogin proves --edge/--context, parsed by cobra at
// Execute time, are actually seen by runLogin (the stub edge is hit and the
// named context is written with that edge). Uses the agent path so no terminal
// prompt is needed. This guards the flag-timing wiring in main.go / NewCmd.
func TestLoginCmdFlagsReachRunLogin(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_OWNER_SEED", "owner-seed-32-bytes-padded-xxxxx")
	var hit int32
	srv := stubHeraldAgent(t, &hit)

	gf := &GlobalFlags{}
	root := newTestRoot(gf)

	root.SetArgs([]string{
		"auth", "login", "--agent",
		"--agent-id", "agent-123", "--slug", "shadow",
		"--edge", srv.URL, "--context", "dev",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if atomic.LoadInt32(&hit) == 0 {
		t.Fatalf("token endpoint never hit — edge flag did not reach runLogin")
	}
	c, _ := config.Load()
	ctx, ok := c.Current()
	if !ok || c.CurrentContext != "dev" {
		t.Fatalf("context flag did not reach runLogin: %+v", c)
	}
	if ctx.Edge != srv.URL {
		t.Fatalf("edge flag did not reach runLogin: got %q want %q", ctx.Edge, srv.URL)
	}
	if ctx.Identity.Subject != "agent-123" || ctx.Identity.Kind != "agent" {
		t.Fatalf("identity not decoded from access token: %+v", ctx.Identity)
	}
}
