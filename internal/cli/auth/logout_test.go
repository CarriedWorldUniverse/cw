package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/zalando/go-keyring"
)

func TestLogoutClearsTokens(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_endpoint":"` + srv.URL + `/herald/token","revocation_endpoint":"` + srv.URL + `/herald/revoke"}`))
	})
	revoked := false
	mux.HandleFunc("POST /herald/revoke", func(w http.ResponseWriter, _ *http.Request) { revoked = true; w.WriteHeader(200) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{CurrentContext: "dev", Contexts: map[string]config.Context{
		"dev": {Edge: srv.URL, Identity: config.Identity{Kind: "human", Subject: "u1"}},
	}}
	_ = cfg.Save()
	st := tokenstore.New(srv.URL, "dev", "u1")
	_ = st.SaveRefresh("r-1")
	_ = st.SaveAccess("a-1", time.Now().Add(time.Hour))

	if err := runLogout(&GlobalFlags{}); err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if !revoked {
		t.Fatal("refresh token was not revoked at herald")
	}
	if _, err := st.Refresh(); err == nil {
		t.Fatal("refresh token not cleared from keychain")
	}
}
