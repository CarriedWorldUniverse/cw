package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/zalando/go-keyring"
)

// reuse the oidc stub shape: discovery + refresh + a product route.
func stub(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_endpoint":"` + srv.URL + `/herald/token"}`))
	})
	mux.HandleFunc("POST /herald/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") == "r-old" {
			_, _ = w.Write([]byte(`{"access_token":"a-fresh","expires_in":600,"refresh_token":"r-new2"}`))
			return
		}
		w.WriteHeader(401)
	})
	mux.HandleFunc("GET /ledger/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a-fresh" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`pong`))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSilentRefreshThenCall(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	srv := stub(t)
	ts := tokenstore.New(srv.URL, "dev", "u1")
	_ = ts.SaveRefresh("r-old")
	// Cache an already-expired access token to force a refresh.
	_ = ts.SaveAccess("a-stale", time.Now().Add(-time.Minute))

	c := New(srv.URL, ts, oidc.New(srv.URL))
	resp, body, err := c.Get(context.Background(), "ledger", "/ping")
	if err != nil || resp.StatusCode != 200 || string(body) != "pong" {
		t.Fatalf("Get after silent refresh: %v status=%d body=%q", err, resp.StatusCode, body)
	}
	// The refreshed access token is now cached.
	at, _, _ := ts.Access()
	if at != "a-fresh" {
		t.Fatalf("access not refreshed/cached: %q", at)
	}
}

// TestDo401RefreshRetry covers the reactive path: a cached token that LOOKS
// valid (not yet expired) but the server rejects with 401 → one silent refresh
// + retry succeeds.
func TestDo401RefreshRetry(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	srv := stub(t)
	ts := tokenstore.New(srv.URL, "dev", "u1")
	_ = ts.SaveRefresh("r-old")
	// Not-yet-expired but server-rejected (e.g. revoked) access token.
	_ = ts.SaveAccess("a-stale", time.Now().Add(10*time.Minute))

	c := New(srv.URL, ts, oidc.New(srv.URL))
	resp, body, err := c.Get(context.Background(), "ledger", "/ping")
	if err != nil || resp.StatusCode != 200 || string(body) != "pong" {
		t.Fatalf("Get after 401-retry: %v status=%d body=%q", err, resp.StatusCode, body)
	}
}
