package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func stubHerald(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("GET /herald/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_endpoint":"` + srv.URL + `/herald/token","revocation_endpoint":"` + srv.URL + `/herald/revoke"}`))
	})
	mux.HandleFunc("POST /herald/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "password":
			if r.Form.Get("username") != "alice" || r.Form.Get("password") != "pw" {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
		case "refresh_token":
			if r.Form.Get("refresh_token") != "r-old" {
				w.WriteHeader(401)
				return
			}
		case "urn:ietf:params:oauth:grant-type:jwt-bearer":
			if r.Form.Get("assertion") == "" {
				w.WriteHeader(400)
				return
			}
		}
		_, _ = w.Write([]byte(`{"access_token":"a-new","token_type":"Bearer","expires_in":600,"refresh_token":"r-new"}`))
	})
	mux.HandleFunc("POST /herald/revoke", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverAndGrants(t *testing.T) {
	srv := stubHerald(t)
	c := New(srv.URL)
	ctx := context.Background()

	d, err := c.Discover(ctx)
	if err != nil || d.TokenEndpoint == "" {
		t.Fatalf("Discover: %v %+v", err, d)
	}

	tok, err := c.PasswordGrant(ctx, "alice", "pw")
	if err != nil || tok.AccessToken != "a-new" || tok.RefreshToken != "r-new" || tok.ExpiresIn != 600 {
		t.Fatalf("PasswordGrant: %v %+v", err, tok)
	}
	if _, err := c.PasswordGrant(ctx, "alice", "wrong"); err == nil {
		t.Fatal("bad password should error")
	}

	tok, err = c.RefreshGrant(ctx, "r-old")
	if err != nil || tok.AccessToken != "a-new" {
		t.Fatalf("RefreshGrant: %v %+v", err, tok)
	}
	if _, err := c.RefreshGrant(ctx, "r-stale"); err == nil {
		t.Fatal("stale refresh should error")
	}

	tok, err = c.JWTBearerGrant(ctx, "signed.assertion.jws")
	if err != nil || tok.AccessToken != "a-new" {
		t.Fatalf("JWTBearerGrant: %v %+v", err, tok)
	}
	if _, err := c.JWTBearerGrant(ctx, ""); err == nil {
		t.Fatal("empty assertion should error")
	}

	if err := c.Revoke(ctx, "r-new"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
}
