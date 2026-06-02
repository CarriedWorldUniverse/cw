package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/zalando/go-keyring"
)

func stubHerald(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_endpoint":"` + srv.URL + `/herald/token"}`))
	})
	mux.HandleFunc("POST /herald/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "password" && r.Form.Get("password") == "pw" {
			// access token whose claims say sub=u1, kind=human, org=acme.
			at := "x." + b64(`{"sub":"u1","kind":"human","org":"acme","scope":"issue:read","exp":9999999999}`) + ".y"
			_, _ = w.Write([]byte(`{"access_token":"` + at + `","expires_in":600,"refresh_token":"r-1"}`))
			return
		}
		w.WriteHeader(401)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLoginHuman(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	srv := stubHerald(t)

	// Inject credentials (bypass the terminal prompt).
	err := runLogin(context.Background(), loginOpts{
		edge: srv.URL, contextName: "dev",
		username: "alice@x", password: "pw",
	})
	if err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	c, _ := config.Load()
	ctx, ok := c.Current()
	if !ok || c.CurrentContext != "dev" || ctx.Identity.Subject != "u1" || ctx.Identity.Display != "alice@x" {
		t.Fatalf("context not written: %+v", c)
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
