package tenant

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestOnboardWiring drives cobra Execute against a stub herald and proves the
// full composition: create org (with default = all products), then create the
// owner human carrying the role:org-owner grant, in that order.
func TestOnboardWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hits []string
	var humanBody struct {
		DisplayName string   `json:"display_name"`
		Scopes      []string `json:"scopes"`
	}
	var orgBody struct {
		Name     string   `json:"name"`
		Products []string `json:"products"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs", func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "create-org")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &orgBody)
		_, _ = w.Write([]byte(`{"id":"o1","name":"acme"}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/humans", func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "create-human")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &humanBody)
		_, _ = w.Write([]byte(`{"id":"u1","display_name":"alice@acme.test","org":"o1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok", JSON: true}
	cmd := NewCmd(gf)
	out := &strings.Builder{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"onboard", "acme", "--owner", "alice@acme.test"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if strings.Join(hits, ",") != "create-org,create-human" {
		t.Fatalf("call order = %v, want create-org then create-human", hits)
	}
	if orgBody.Name != "acme" {
		t.Fatalf("org name = %q", orgBody.Name)
	}
	if len(orgBody.Products) != 0 {
		t.Fatalf("default products must be empty (= all enabled), got %v", orgBody.Products)
	}
	if len(humanBody.Scopes) != 1 || humanBody.Scopes[0] != "role:org-owner" {
		t.Fatalf("owner must be granted role:org-owner, got %v", humanBody.Scopes)
	}

	var res onboardResult
	if err := json.Unmarshal([]byte(out.String()), &res); err != nil {
		t.Fatalf("decode result: %v (%s)", err, out.String())
	}
	if res.Org != "o1" || res.Owner != "u1" || res.Role != "role:org-owner" {
		t.Fatalf("result = %+v", res)
	}
	if res.PasswordSet {
		t.Fatal("password must not be set without --owner-password-stdin")
	}
	if res.ConsoleURL == "" || res.CLIDownloadURL == "" {
		t.Fatalf("console/download URLs must be printed, got %+v", res)
	}
}

// TestOnboardSetsPassword: with --owner-password-stdin the owner's password is
// set after creation (a third call), and the result reflects it.
func TestOnboardSetsPassword(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hits []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		hits = append(hits, "create-org")
		_, _ = w.Write([]byte(`{"id":"o1","name":"acme"}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/humans", func(w http.ResponseWriter, _ *http.Request) {
		hits = append(hits, "create-human")
		_, _ = w.Write([]byte(`{"id":"u1","display_name":"alice@acme.test","org":"o1"}`))
	})
	mux.HandleFunc("POST /herald/api/humans/u1/password", func(w http.ResponseWriter, _ *http.Request) {
		hits = append(hits, "set-password")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok", JSON: true}
	cmd := NewCmd(gf)
	out := &strings.Builder{}
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader("hunter2hunter2\n"))
	cmd.SetArgs([]string{"onboard", "acme", "--owner", "alice@acme.test", "--owner-password-stdin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Join(hits, ",") != "create-org,create-human,set-password" {
		t.Fatalf("call order = %v", hits)
	}
	var res onboardResult
	_ = json.Unmarshal([]byte(out.String()), &res)
	if !res.PasswordSet {
		t.Fatal("PasswordSet must be true")
	}
}

// TestOnboardRequiresOwner: missing --owner fails before any network call.
func TestOnboardRequiresOwner(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"onboard", "acme"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected --owner required error")
	}
	if called {
		t.Fatal("must not hit the server when --owner is missing")
	}
}
