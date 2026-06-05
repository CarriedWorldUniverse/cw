package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
)

func TestAPIRequestConstructionAndSeamAuth(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")

	var sawSeam bool
	seam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSeam = true
		if r.Method != http.MethodPost || r.URL.Path != "/api/agent/credential.fetch" {
			t.Fatalf("seam request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
			t.Fatalf("seam Authorization = %q", got)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode seam body: %v", err)
		}
		if req["kind"] != "git" || req["host"] != "github.com" {
			t.Fatalf("seam body = %#v", req)
		}
		_, _ = w.Write([]byte(`{"bundle":{"username":"agent","password":"ghp_pat"}}`))
	}))
	defer seam.Close()
	t.Setenv("CW_SEAM_URL", seam.URL)

	var sawAPI bool
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAPI = true
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q", r.Method)
		}
		if r.URL.Path != "/repos/OWNER/REPO/pulls" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token ghp_pat" {
			t.Fatalf("api Authorization = %q", got)
		}
		if got := r.Header.Get("X-Test"); got != "ok" {
			t.Fatalf("X-Test = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode api body: %v", err)
		}
		if body["title"] != "ship" || body["draft"] != true || body["empty"] != nil || body["raw"] != "123" {
			t.Fatalf("body = %#v", body)
		}
		if got, _ := body["number"].(float64); got != 42 {
			t.Fatalf("number = %#v", body["number"])
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apiSrv.Close()
	restore := overrideAPI(t, apiSrv)
	defer restore()

	cmd := NewCmd(&cmdutil.GlobalFlags{Token: "session-token"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"repos/OWNER/REPO/pulls",
		"-X", "POST",
		"-f", "title=ship",
		"-f", "number=42",
		"-f", "draft=true",
		"-f", "empty=null",
		"-F", "raw=123",
		"-H", "X-Test: ok",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !sawSeam || !sawAPI {
		t.Fatalf("sawSeam=%v sawAPI=%v", sawSeam, sawAPI)
	}
	if out.String() != `{"ok":true}` {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestAPIHostResolutionUsesPrimaryDefault(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	cfg := &config.Config{Git: config.Git{PrimaryHost: "github"}, Contexts: map[string]config.Context{}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	seam := seamServer(t, "session-token", "github.com", "ghp_pat")
	defer seam.Close()
	t.Setenv("CW_SEAM_URL", seam.URL)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"login":"agent"}`))
	}))
	defer apiSrv.Close()
	restore := overrideAPI(t, apiSrv)
	defer restore()

	cmd := NewCmd(&cmdutil.GlobalFlags{Token: "session-token"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"user"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.String() != `{"login":"agent"}` {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestAPIExplicitHost(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	seam := seamServer(t, "session-token", "github.com", "ghp_pat")
	defer seam.Close()
	t.Setenv("CW_SEAM_URL", seam.URL)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer apiSrv.Close()
	restore := overrideAPI(t, apiSrv)
	defer restore()

	cmd := NewCmd(&cmdutil.GlobalFlags{Token: "session-token"})
	cmd.SetArgs([]string{"--host", "github", "rate_limit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestAPICairnStub(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")

	cmd := NewCmd(&cmdutil.GlobalFlags{Token: "session-token"})
	cmd.SetArgs([]string{"--host", "cairn", "repos/acme/demo/pulls"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cairn api not yet supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPINon2xxIncludesStatusAndBody(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	seam := seamServer(t, "session-token", "github.com", "ghp_pat")
	defer seam.Close()
	t.Setenv("CW_SEAM_URL", seam.URL)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"bad"}`, http.StatusTeapot)
	}))
	defer apiSrv.Close()
	restore := overrideAPI(t, apiSrv)
	defer restore()

	cmd := NewCmd(&cmdutil.GlobalFlags{Token: "session-token"})
	cmd.SetArgs([]string{"user"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "418 I'm a teapot") || !strings.Contains(err.Error(), `{"message":"bad"}`) {
		t.Fatalf("err = %v", err)
	}
}

func seamServer(t *testing.T, wantToken, wantHost, pat string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantToken {
			t.Fatalf("seam Authorization = %q", got)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode seam body: %v", err)
		}
		if req["host"] != wantHost {
			t.Fatalf("host = %q, want %q", req["host"], wantHost)
		}
		_, _ = w.Write([]byte(`{"bundle":{"username":"agent","password":"` + pat + `"}}`))
	}))
}

func overrideAPI(t *testing.T, srv *httptest.Server) func() {
	t.Helper()
	oldBase := githubAPIBase
	oldClient := httpClient
	githubAPIBase = srv.URL
	httpClient = srv.Client()
	return func() {
		githubAPIBase = oldBase
		httpClient = oldClient
	}
}
