# `cw` core + `cw auth` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single `cw` Go binary that authenticates a human or agent against the CWB platform (via herald) and keeps the session fresh, exposing `cw auth {login,logout,whoami,status,switch,token}` on a shared core that every later command group will reuse.

**Architecture:** `cw` is anchored on ONE configured URL — the **interchange edge**. Auth goes through the only tokenless route (`<edge>/herald` OIDC bootstrap: discovery → password/jwt-bearer grant → access + refresh token); product calls (`<edge>/cairn|ledger|knowledge`) carry the herald bearer. A *context* is `{edge, identity}`. The refresh token lives in the OS keychain; the access token is cached (0600) and silently refreshed before expiry. Packages: `config` (contexts), `tokenstore` (keychain + access cache), `oidc` (discovery + grants), `identity` (human prompt + agent casket assertion), `client` (edge-anchored HTTP with silent refresh — the seam later command groups build on), `cli/auth` (the commands).

**Tech Stack:** Go 1.26, `spf13/cobra`, `gopkg.in/yaml.v3`, `zalando/go-keyring`, `CarriedWorldUniverse/casket-go`, `go-jose/go-jose/v4`, `golang.org/x/term`.

This is sub-project **#0b** of the CW CLI suite. Spec: `cw/docs/superpowers/specs/2026-06-02-cw-core-auth-design.md` (Part B). **#0a (herald refresh tokens) is DONE + live on dMon** — the wire shapes below match the deployed herald. Later command groups (`repo`/`pr`, `issue`, `kb`, `org`) are separate cycles consuming `internal/client`.

---

## Live herald contract (what `cw` codes against)

All under the configured edge `E`:
- **Discovery:** `GET E/herald/.well-known/openid-configuration` → `{token_endpoint, jwks_uri, revocation_endpoint, grant_types_supported}`. `token_endpoint` = `E/herald/token`, `revocation_endpoint` = `E/herald/revoke`.
- **Password grant:** `POST token_endpoint` form `grant_type=password&username=<email|id>&password=<pw>` → `{access_token, token_type:"Bearer", expires_in:600, refresh_token}`.
- **jwt-bearer grant (agent):** form `grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer&assertion=<JWS>` → same response. Assertion = EdDSA-signed JWT, type "JWT", claims `{iss:agentID, sub:agentID, aud:token_endpoint, iat, exp}` signed with `casket.DeriveAgentKey(seed, slug)`'s private key.
- **Refresh grant:** form `grant_type=refresh_token&refresh_token=<tok>` → new `{access_token, refresh_token, ...}`. Old token is rotated away (reuse → 401).
- **Revoke:** `POST revocation_endpoint` form `token=<refresh>` → always 200.
- **Product routes:** `E/cairn/…`, `E/ledger/…`, `E/knowledge/…` with `Authorization: Bearer <access>`.
- Access tokens are EdDSA JWTs; claims include `sub`, `kind`, `org`, `scope` (space-joined), `products`, `exp`. `cw` reads these locally for display (no signature verification needed — the token came from herald).

---

## Task 1: repo scaffold + config package

**Files:**
- Create: `go.mod`, `cmd/cw/main.go`, `internal/config/config.go`, `internal/config/config_test.go`

- [ ] **Step 1: Initialise the module**

Run (in `/Users/jacinta/Source/cw`):
```bash
go mod init github.com/CarriedWorldUniverse/cw
go get github.com/spf13/cobra@latest gopkg.in/yaml.v3@latest github.com/zalando/go-keyring@latest github.com/CarriedWorldUniverse/casket-go@latest github.com/go-jose/go-jose/v4@latest golang.org/x/term@latest
```
Expected: `go.mod` + `go.sum` created with those deps.

- [ ] **Step 2: Write the failing config test**

`internal/config/config_test.go`:
```go
package config

import (
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CW_CONFIG_DIR", dir)

	// Missing file -> empty config, no error.
	c, err := Load()
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if len(c.Contexts) != 0 || c.CurrentContext != "" {
		t.Fatalf("fresh config not empty: %+v", c)
	}

	c.Upsert("dev", Context{Edge: "http://edge:8080", Identity: Identity{Kind: "human", Subject: "u1", Display: "a@x"}})
	c.CurrentContext = "dev"
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cur, ok := got.Current()
	if !ok || cur.Edge != "http://edge:8080" || cur.Identity.Subject != "u1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if filepath.Dir(path()) != dir {
		t.Fatalf("path() = %q, want under %q", path(), dir)
	}
}

func TestResolvePrecedence(t *testing.T) {
	c := &Config{CurrentContext: "dev", Contexts: map[string]Context{
		"dev":  {Edge: "http://dev:8080"},
		"prod": {Edge: "http://prod:8080"},
	}}
	// Explicit name wins.
	ctx, name, err := c.Resolve("prod", "")
	if err != nil || name != "prod" || ctx.Edge != "http://prod:8080" {
		t.Fatalf("explicit name: %v %q %+v", err, name, ctx)
	}
	// --edge override with no name -> ephemeral context off current.
	ctx, _, err = c.Resolve("", "http://override:9000")
	if err != nil || ctx.Edge != "http://override:9000" {
		t.Fatalf("edge override: %v %+v", err, ctx)
	}
	// Default -> current.
	ctx, name, _ = c.Resolve("", "")
	if name != "dev" || ctx.Edge != "http://dev:8080" {
		t.Fatalf("default: %q %+v", name, ctx)
	}
}
```

- [ ] **Step 3: Run — expect FAIL (package not built)**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/config/`
Expected: build error (undefined `Config`, `Load`, etc.).

- [ ] **Step 4: Implement the config package**

`internal/config/config.go`:
```go
// Package config is cw's on-disk configuration: named contexts (each an edge
// URL + the identity logged in there) and the current context. It holds NO
// secrets — refresh tokens live in the OS keychain, access tokens in a separate
// cache, agent seeds in env/keychain. File: $CW_CONFIG_DIR (or
// $XDG_CONFIG_HOME/cw, or ~/.config/cw)/config.yaml, mode 0600.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Identity is who is logged in to a context. Subject/Display are filled from the
// access token after login; Slug is set for agents (the casket key slug).
type Identity struct {
	Kind    string `yaml:"kind"`              // "human" | "agent"
	Subject string `yaml:"subject,omitempty"` // herald user id
	Display string `yaml:"display,omitempty"` // email / display name
	Slug    string `yaml:"slug,omitempty"`    // agent only
}

// Context binds an edge URL to an identity.
type Context struct {
	Edge     string   `yaml:"edge"`
	Identity Identity `yaml:"identity"`
}

// Config is the whole file.
type Config struct {
	CurrentContext string             `yaml:"current-context"`
	Contexts       map[string]Context `yaml:"contexts"`
}

// Dir returns cw's config directory, honouring CW_CONFIG_DIR, then
// XDG_CONFIG_HOME/cw, then ~/.config/cw.
func Dir() string {
	if d := os.Getenv("CW_CONFIG_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "cw")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cw")
}

func path() string { return filepath.Join(Dir(), "config.yaml") }

// Load reads the config. A missing file yields an empty (usable) Config.
func Load() (*Config, error) {
	b, err := os.ReadFile(path())
	if errors.Is(err, os.ErrNotExist) {
		return &Config{Contexts: map[string]Context{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	if c.Contexts == nil {
		c.Contexts = map[string]Context{}
	}
	return &c, nil
}

// Save writes the config 0600, creating the directory if needed.
func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path(), b, 0o600); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	return nil
}

// Upsert adds or replaces a context.
func (c *Config) Upsert(name string, ctx Context) {
	if c.Contexts == nil {
		c.Contexts = map[string]Context{}
	}
	c.Contexts[name] = ctx
}

// Current returns the current context (false if unset/missing).
func (c *Config) Current() (Context, bool) {
	ctx, ok := c.Contexts[c.CurrentContext]
	return ctx, ok
}

// Resolve picks the effective context from flag overrides:
//   - name set  -> that named context (error if unknown)
//   - edge set  -> the current context with Edge replaced (or a bare context)
//   - neither   -> the current context (error if none)
func (c *Config) Resolve(name, edge string) (Context, string, error) {
	if name != "" {
		ctx, ok := c.Contexts[name]
		if !ok {
			return Context{}, "", fmt.Errorf("config: no such context %q", name)
		}
		if edge != "" {
			ctx.Edge = edge
		}
		return ctx, name, nil
	}
	if edge != "" {
		ctx, _ := c.Current()
		ctx.Edge = edge
		return ctx, c.CurrentContext, nil
	}
	ctx, ok := c.Current()
	if !ok {
		return Context{}, "", errors.New("config: no current context (run 'cw auth login --edge <url>')")
	}
	return ctx, c.CurrentContext, nil
}
```

- [ ] **Step 5: Minimal cobra root so the module builds**

`cmd/cw/main.go`:
```go
// Command cw is the CWB platform CLI for humans and agents.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Global flags shared by all subcommands (precedence: flag > env > current context).
var (
	flagContext  string
	flagEdge     string
	flagToken    string
	flagIdentity string
	flagJSON     bool
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cw",
		Short:         "CWB platform CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	p := root.PersistentFlags()
	p.StringVar(&flagContext, "context", os.Getenv("CW_CONTEXT"), "context name")
	p.StringVar(&flagEdge, "edge", os.Getenv("CW_EDGE"), "interchange edge URL (override)")
	p.StringVar(&flagToken, "token", os.Getenv("CW_TOKEN"), "use this bearer token directly (skip the token store)")
	p.StringVar(&flagIdentity, "identity", os.Getenv("CW_IDENTITY"), "agent identity file (for --agent login)")
	p.BoolVar(&flagJSON, "json", false, "machine-readable JSON output")
	// auth command group is registered in Task 6.
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cw: "+err.Error())
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/config/ -v`
Expected: build OK; `TestLoadSaveRoundTrip` + `TestResolvePrecedence` PASS.

- [ ] **Step 7: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: module scaffold + config package (named contexts, no secrets)"
```

---

## Task 2: tokenstore — keychain refresh + access cache

**Files:**
- Create: `internal/tokenstore/tokenstore.go`, `internal/tokenstore/tokenstore_test.go`

- [ ] **Step 1: Write the failing test**

`internal/tokenstore/tokenstore_test.go`:
```go
package tokenstore

import (
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestRefreshAndAccessRoundTrip(t *testing.T) {
	keyring.MockInit() // in-memory keychain
	dir := t.TempDir()
	t.Setenv("CW_CONFIG_DIR", dir)
	s := New("http://edge:8080", "ctx", "u1")

	if err := s.SaveRefresh("rtok-123"); err != nil {
		t.Fatalf("SaveRefresh: %v", err)
	}
	got, err := s.Refresh()
	if err != nil || got != "rtok-123" {
		t.Fatalf("Refresh = %q,%v", got, err)
	}

	exp := time.Now().Add(10 * time.Minute)
	if err := s.SaveAccess("atok-abc", exp); err != nil {
		t.Fatalf("SaveAccess: %v", err)
	}
	tok, gotExp, err := s.Access()
	if err != nil || tok != "atok-abc" {
		t.Fatalf("Access = %q,%v", tok, err)
	}
	if gotExp.Unix() != exp.Unix() {
		t.Fatalf("exp = %v want %v", gotExp, exp)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := s.Refresh(); err == nil {
		t.Fatal("Refresh after Clear should error")
	}
	if _, _, err := s.Access(); err == nil {
		t.Fatal("Access after Clear should error")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/tokenstore/`
Expected: build error (undefined `New`).

- [ ] **Step 3: Implement**

`internal/tokenstore/tokenstore.go`:
```go
// Package tokenstore persists a context's tokens: the durable refresh token in
// the OS keychain (go-keyring; macOS Keychain / Linux Secret Service, encrypted
// file fallback) and the short-lived access token (+ its expiry) in a 0600
// cache file under the config dir. Keyed by {edge, subject} so multiple
// identities/edges don't collide.
package tokenstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/zalando/go-keyring"
)

const keyringService = "cw"

// Store is one context's token storage.
type Store struct {
	edge    string
	context string
	subject string
}

// New builds a Store for an edge + context name + subject (the keychain key).
func New(edge, context, subject string) *Store {
	return &Store{edge: edge, context: context, subject: subject}
}

func (s *Store) keyringKey() string { return s.edge + "|" + s.subject }

func (s *Store) accessPath() string {
	return filepath.Join(config.Dir(), "tokens", s.context+".json")
}

// SaveRefresh stores the refresh token in the keychain.
func (s *Store) SaveRefresh(token string) error {
	if err := keyring.Set(keyringService, s.keyringKey(), token); err != nil {
		return fmt.Errorf("tokenstore: keychain set: %w", err)
	}
	return nil
}

// Refresh reads the refresh token from the keychain.
func (s *Store) Refresh() (string, error) {
	tok, err := keyring.Get(keyringService, s.keyringKey())
	if err != nil {
		return "", fmt.Errorf("tokenstore: keychain get: %w", err)
	}
	return tok, nil
}

type accessCache struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// SaveAccess caches the access token + its absolute expiry (0600).
func (s *Store) SaveAccess(token string, expiresAt time.Time) error {
	if err := os.MkdirAll(filepath.Dir(s.accessPath()), 0o700); err != nil {
		return fmt.Errorf("tokenstore: mkdir: %w", err)
	}
	b, _ := json.Marshal(accessCache{AccessToken: token, ExpiresAt: expiresAt})
	if err := os.WriteFile(s.accessPath(), b, 0o600); err != nil {
		return fmt.Errorf("tokenstore: write access: %w", err)
	}
	return nil
}

// Access returns the cached access token + expiry, or an error if absent.
func (s *Store) Access() (string, time.Time, error) {
	b, err := os.ReadFile(s.accessPath())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("tokenstore: read access: %w", err)
	}
	var c accessCache
	if err := json.Unmarshal(b, &c); err != nil {
		return "", time.Time{}, fmt.Errorf("tokenstore: parse access: %w", err)
	}
	return c.AccessToken, c.ExpiresAt, nil
}

// Clear removes both tokens. Best-effort on a missing access cache.
func (s *Store) Clear() error {
	kerr := keyring.Delete(keyringService, s.keyringKey())
	if kerr != nil && !errors.Is(kerr, keyring.ErrNotFound) {
		return fmt.Errorf("tokenstore: keychain delete: %w", kerr)
	}
	if err := os.Remove(s.accessPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tokenstore: remove access: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/tokenstore/ -v`
Expected: `TestRefreshAndAccessRoundTrip` PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: tokenstore — refresh token in OS keychain, access token cached 0600"
```

---

## Task 3: oidc — discovery + grant/revoke calls

**Files:**
- Create: `internal/oidc/oidc.go`, `internal/oidc/oidc_test.go`

- [ ] **Step 1: Write the failing test**

`internal/oidc/oidc_test.go`:
```go
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
	if err := c.Revoke(ctx, "r-new"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/oidc/`
Expected: build error (undefined `New`).

- [ ] **Step 3: Implement**

`internal/oidc/oidc.go`:
```go
// Package oidc talks to herald's OIDC bootstrap endpoints through the edge: it
// discovers the token/revocation endpoints and performs the password,
// jwt-bearer, and refresh_token grants plus RFC 7009 revocation. These are the
// only tokenless routes through interchange. herald is reached at <edge>/herald.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is anchored on an interchange edge URL.
type Client struct {
	edge string
	hc   *http.Client
}

// New builds a client for an edge (trailing slash trimmed).
func New(edge string) *Client {
	return &Client{edge: strings.TrimRight(edge, "/"), hc: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) heraldBase() string { return c.edge + "/herald" }

// Discovery is the subset of the OIDC discovery doc cw uses.
type Discovery struct {
	TokenEndpoint      string `json:"token_endpoint"`
	RevocationEndpoint string `json:"revocation_endpoint"`
	JWKSURI            string `json:"jwks_uri"`
}

// Discover fetches the OIDC discovery document from the edge.
func (c *Client) Discover(ctx context.Context) (Discovery, error) {
	u := c.heraldBase() + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return Discovery{}, fmt.Errorf("oidc: cannot reach herald at %s: %w", c.edge, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return Discovery{}, fmt.Errorf("oidc: discovery status %d: %s", resp.StatusCode, body)
	}
	var d Discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return Discovery{}, fmt.Errorf("oidc: discovery parse: %w", err)
	}
	if d.TokenEndpoint == "" {
		return Discovery{}, fmt.Errorf("oidc: discovery missing token_endpoint")
	}
	return d, nil
}

// Token is a grant response.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (c *Client) tokenURL(ctx context.Context) (string, error) {
	d, err := c.Discover(ctx)
	if err != nil {
		return "", err
	}
	return d.TokenEndpoint, nil
}

// PasswordGrant runs grant_type=password (human login).
func (c *Client) PasswordGrant(ctx context.Context, username, password string) (Token, error) {
	tu, err := c.tokenURL(ctx)
	if err != nil {
		return Token{}, err
	}
	return c.grant(ctx, tu, url.Values{
		"grant_type": {"password"}, "username": {username}, "password": {password},
	}, "login")
}

// JWTBearerGrant runs grant_type=jwt-bearer (agent login) with a signed assertion.
func (c *Client) JWTBearerGrant(ctx context.Context, assertion string) (Token, error) {
	tu, err := c.tokenURL(ctx)
	if err != nil {
		return Token{}, err
	}
	return c.grant(ctx, tu, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion},
	}, "assertion login")
}

// RefreshGrant runs grant_type=refresh_token.
func (c *Client) RefreshGrant(ctx context.Context, refreshToken string) (Token, error) {
	tu, err := c.tokenURL(ctx)
	if err != nil {
		return Token{}, err
	}
	return c.grant(ctx, tu, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refreshToken},
	}, "refresh")
}

// TokenEndpoint exposes the discovered token endpoint (agents need it as the
// assertion audience).
func (c *Client) TokenEndpoint(ctx context.Context) (string, error) { return c.tokenURL(ctx) }

func (c *Client) grant(ctx context.Context, tokenURL string, form url.Values, what string) (Token, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("oidc: %s: %w", what, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return Token{}, fmt.Errorf("oidc: %s rejected (status %d)", what, resp.StatusCode)
	}
	var t Token
	if err := json.Unmarshal(body, &t); err != nil {
		return Token{}, fmt.Errorf("oidc: %s decode: %w", what, err)
	}
	if t.AccessToken == "" {
		return Token{}, fmt.Errorf("oidc: %s: empty access_token", what)
	}
	return t, nil
}

// Revoke revokes a refresh token (RFC 7009; herald always answers 200).
func (c *Client) Revoke(ctx context.Context, refreshToken string) error {
	d, err := c.Discover(ctx)
	if err != nil {
		return err
	}
	ru := d.RevocationEndpoint
	if ru == "" {
		ru = c.heraldBase() + "/revoke"
	}
	form := url.Values{"token": {refreshToken}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ru, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("oidc: revoke: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("oidc: revoke status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/oidc/ -v`
Expected: `TestDiscoverAndGrants` PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: oidc client — discovery + password/jwt-bearer/refresh grants + revoke"
```

---

## Task 4: identity — human prompt + agent casket assertion + JWT claim decode

**Files:**
- Create: `internal/identity/identity.go`, `internal/identity/identity_test.go`

- [ ] **Step 1: Write the failing test**

`internal/identity/identity_test.go`:
```go
package identity

import (
	"testing"

	casket "github.com/CarriedWorldUniverse/casket-go"
	jose "github.com/go-jose/go-jose/v4"
)

func TestAgentAssertionVerifies(t *testing.T) {
	seed := []byte("owner-seed-32-bytes-padded-xxxxx")
	_, pub, err := casket.DeriveAgentKey(seed, "shadow")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	assertion, err := AgentAssertion(seed, "shadow", "agent-123", "http://edge:8080/herald/token")
	if err != nil {
		t.Fatalf("AgentAssertion: %v", err)
	}
	// Verify the assertion against the derived public key (what herald does).
	jws, err := jose.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	payload, err := jws.Verify(pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	claims := DecodeClaimsBytes(payload)
	if claims["iss"] != "agent-123" || claims["sub"] != "agent-123" || claims["aud"] != "http://edge:8080/herald/token" {
		t.Fatalf("claims: %+v", claims)
	}
}

func TestDecodeAccessClaims(t *testing.T) {
	// header.payload.sig where payload = base64url({"sub":"u1","kind":"human","scope":"a b"})
	tok := "x." + b64url(`{"sub":"u1","kind":"human","scope":"a b","exp":111}`) + ".y"
	claims, err := DecodeAccessClaims(tok)
	if err != nil || claims["sub"] != "u1" || claims["kind"] != "human" {
		t.Fatalf("DecodeAccessClaims: %v %+v", err, claims)
	}
}
```

Add a tiny test helper `b64url` at the bottom of the test file:
```go
func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
```
(and `import "encoding/base64"` in the test).

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/identity/`
Expected: build error (undefined `AgentAssertion`).

- [ ] **Step 3: Implement**

`internal/identity/identity.go`:
```go
// Package identity produces the credentials cw presents to herald: it prompts a
// human for email+password (no-echo), signs an agent's casket jwt-bearer
// assertion, and decodes (without verifying) access-token claims for display.
package identity

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	casket "github.com/CarriedWorldUniverse/casket-go"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/term"
)

// PromptHuman reads an email/username + password from the terminal (password
// not echoed). Used by `cw auth login` for humans.
func PromptHuman(in *os.File) (username, password string, err error) {
	fmt.Fprint(os.Stderr, "Email: ")
	var u string
	if _, err := fmt.Fscanln(in, &u); err != nil {
		return "", "", fmt.Errorf("identity: read username: %w", err)
	}
	fmt.Fprint(os.Stderr, "Password: ")
	pw, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", "", fmt.Errorf("identity: read password: %w", err)
	}
	return strings.TrimSpace(u), string(pw), nil
}

// AgentAssertion derives the agent's casket key from (seed, slug) and signs an
// RFC 7523 jwt-bearer assertion (iss=sub=agentID, aud=tokenURL, 2-minute exp).
// now defaults to time.Now; injectable for tests via AgentAssertionAt.
func AgentAssertion(seed []byte, slug, agentID, tokenURL string) (string, error) {
	return AgentAssertionAt(seed, slug, agentID, tokenURL, time.Now())
}

// AgentAssertionAt is AgentAssertion with an explicit clock.
func AgentAssertionAt(seed []byte, slug, agentID, tokenURL string, now time.Time) (string, error) {
	if len(seed) == 0 || slug == "" || agentID == "" || tokenURL == "" {
		return "", errors.New("identity: seed, slug, agentID, tokenURL all required")
	}
	priv, _, err := casket.DeriveAgentKey(seed, slug)
	if err != nil {
		return "", fmt.Errorf("identity: derive key: %w", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.EdDSA, Key: priv},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", fmt.Errorf("identity: signer: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"iss": agentID, "sub": agentID, "aud": tokenURL,
		"iat": now.Unix(), "exp": now.Add(2 * time.Minute).Unix(),
	})
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("identity: sign: %w", err)
	}
	return obj.CompactSerialize()
}

// DecodeAccessClaims decodes a JWT's claim set WITHOUT verifying the signature
// (the token came from herald; cw only reads it for display + expiry). Returns
// the claims map.
func DecodeAccessClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("identity: not a JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("identity: decode claims: %w", err)
	}
	return DecodeClaimsBytes(raw), nil
}

// DecodeClaimsBytes unmarshals a JSON claim set (helper shared with tests).
func DecodeClaimsBytes(raw []byte) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/identity/ -v`
Expected: `TestAgentAssertionVerifies` + `TestDecodeAccessClaims` PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: identity — human prompt, agent casket jwt-bearer assertion, claim decode"
```

---

## Task 5: client — edge-anchored HTTP with silent refresh (the seam)

This is the unit every later command group consumes. Given a context + tokenstore + oidc client, it returns a fresh bearer (silently refreshing) and makes authenticated product calls.

**Files:**
- Create: `internal/client/client.go`, `internal/client/client_test.go`

- [ ] **Step 1: Write the failing test**

`internal/client/client_test.go`:
```go
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
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/client/`
Expected: build error (undefined `New`).

- [ ] **Step 3: Implement**

`internal/client/client.go`:
```go
// Package client is the edge-anchored HTTP seam every cw command group uses. It
// keeps the access token fresh (silently running the refresh_token grant before
// expiry, and once more on a 401) and makes authenticated calls to product
// routes under the edge (<edge>/<pillar>/<path>). It needs no knowledge of which
// pillar — callers name the pillar prefix.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// skew refreshes a little before the real expiry to avoid races.
const skew = 60 * time.Second

// ErrReauth means the refresh token is gone/expired/revoked — the caller must
// `cw auth login` again (or, for an agent, re-mint from its seed).
var ErrReauth = errors.New("session expired: run 'cw auth login'")

// Client targets one edge as one identity.
type Client struct {
	edge  string
	store *tokenstore.Store
	oidc  *oidc.Client
	hc    *http.Client
	// staticToken bypasses the store entirely (the --token / CW_TOKEN path).
	staticToken string
}

// New builds a Client from an edge + token store + oidc client.
func New(edge string, store *tokenstore.Store, oc *oidc.Client) *Client {
	return &Client{
		edge: strings.TrimRight(edge, "/"), store: store, oidc: oc,
		hc: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithStaticToken returns a Client that always uses the given bearer and never
// touches the token store (stateless per-invocation use, e.g. ToolRunner agents).
func WithStaticToken(edge, token string) *Client {
	return &Client{edge: strings.TrimRight(edge, "/"), staticToken: token, hc: &http.Client{Timeout: 30 * time.Second}}
}

// bearer returns a currently-valid access token, silently refreshing if the
// cached one is within skew of expiry. Returns ErrReauth when no path to a fresh
// token exists.
func (c *Client) bearer(ctx context.Context) (string, error) {
	if c.staticToken != "" {
		return c.staticToken, nil
	}
	tok, exp, err := c.store.Access()
	if err == nil && time.Until(exp) > skew {
		return tok, nil
	}
	return c.refresh(ctx)
}

func (c *Client) refresh(ctx context.Context) (string, error) {
	rtok, err := c.store.Refresh()
	if err != nil {
		return "", ErrReauth
	}
	t, err := c.oidc.RefreshGrant(ctx, rtok)
	if err != nil {
		return "", ErrReauth
	}
	if err := c.store.SaveRefresh(t.RefreshToken); err != nil {
		return "", err
	}
	exp := time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	if err := c.store.SaveAccess(t.AccessToken, exp); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

// URL builds a product URL: <edge>/<pillar><path> (path begins with "/").
func (c *Client) URL(pillar, path string) string {
	return c.edge + "/" + strings.Trim(pillar, "/") + path
}

// Do executes an authenticated request, injecting the bearer and retrying once
// after a silent refresh on a 401.
func (c *Client) Do(ctx context.Context, method, pillar, path string, body io.Reader) (*http.Response, []byte, error) {
	tok, err := c.bearer(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, raw, err := c.do(ctx, method, c.URL(pillar, path), tok, body)
	if err != nil {
		return nil, nil, err
	}
	// One refresh-and-retry on 401 (token may have been revoked server-side).
	if resp.StatusCode == http.StatusUnauthorized && c.staticToken == "" {
		if fresh, rerr := c.refresh(ctx); rerr == nil {
			return c.do(ctx, method, c.URL(pillar, path), fresh, body)
		}
	}
	return resp, raw, nil
}

// Get is a convenience wrapper.
func (c *Client) Get(ctx context.Context, pillar, path string) (*http.Response, []byte, error) {
	return c.Do(ctx, http.MethodGet, pillar, path, nil)
}

func (c *Client) do(ctx context.Context, method, url, bearer string, body io.Reader) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, nil, fmt.Errorf("client: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("client: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw, nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/client/ -v`
Expected: `TestSilentRefreshThenCall` PASS (the expired cached token triggers a silent refresh, then the product call succeeds with the fresh bearer).

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: client — edge-anchored HTTP seam with silent refresh + 401 retry"
```

---

## Task 6: `cw auth login` (human + agent)

**Files:**
- Create: `internal/cli/auth/auth.go` (the `auth` parent + shared helpers), `internal/cli/auth/login.go`, `internal/cli/auth/login_test.go`
- Modify: `cmd/cw/main.go` (register the auth command)

- [ ] **Step 1: Write the failing test**

`internal/cli/auth/login_test.go`:
```go
package auth

import (
	"context"
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
```
Add the helper at the bottom of the test file:
```go
func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
```
(import `encoding/base64`).

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/cli/auth/`
Expected: build error (undefined `runLogin`).

- [ ] **Step 3: Implement the auth parent + login**

`internal/cli/auth/auth.go`:
```go
// Package auth implements the `cw auth` command group: login, logout, whoami,
// status, switch, token. It wires config + tokenstore + oidc + identity.
package auth

import "github.com/spf13/cobra"

// GlobalFlags carries the root persistent flags the auth commands read.
type GlobalFlags struct {
	Context  string
	Edge     string
	Token    string
	Identity string
	JSON     bool
}

// NewCmd builds the `cw auth` command tree. gf points at the root's flag vars.
func NewCmd(gf *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Authenticate to the CWB platform (herald)"}
	cmd.AddCommand(
		newLoginCmd(gf),
		newLogoutCmd(gf),
		newWhoamiCmd(gf),
		newStatusCmd(gf),
		newSwitchCmd(gf),
		newTokenCmd(gf),
	)
	return cmd
}
```

`internal/cli/auth/login.go`:
```go
package auth

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

type loginOpts struct {
	edge        string
	contextName string
	agent       bool
	// human (injected in tests; otherwise prompted)
	username, password string
	// agent
	agentID, slug string
	seed          []byte
}

func newLoginCmd(gf *GlobalFlags) *cobra.Command {
	var agent bool
	var agentID, slug string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in as a human (password) or agent (--agent, casket assertion)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name := gf.Context
			if name == "" {
				name = "default"
			}
			opts := loginOpts{edge: gf.Edge, contextName: name, agent: agent, agentID: agentID, slug: slug}
			if opts.edge == "" {
				// Fall back to the named context's existing edge.
				if c, err := config.Load(); err == nil {
					if ctx, ok := c.Contexts[name]; ok {
						opts.edge = ctx.Edge
					}
				}
			}
			if opts.edge == "" {
				return fmt.Errorf("no edge: pass --edge <url> on first login")
			}
			if agent {
				if err := loadAgentIdentity(&opts, gf); err != nil {
					return err
				}
			} else {
				u, p, err := identity.PromptHuman(os.Stdin)
				if err != nil {
					return err
				}
				opts.username, opts.password = u, p
			}
			return runLogin(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&agent, "agent", false, "log in as an agent (casket assertion)")
	cmd.Flags().StringVar(&agentID, "agent-id", os.Getenv("CW_AGENT_ID"), "agent herald id")
	cmd.Flags().StringVar(&slug, "slug", os.Getenv("CW_AGENT_SLUG"), "agent casket key slug")
	return cmd
}

// loadAgentIdentity fills agentID/slug/seed from flags/env (the identity file is
// a later refinement; env + flags cover the ToolRunner path now).
func loadAgentIdentity(o *loginOpts, gf *GlobalFlags) error {
	if o.agentID == "" || o.slug == "" {
		return fmt.Errorf("--agent requires --agent-id and --slug (or CW_AGENT_ID/CW_AGENT_SLUG)")
	}
	seed := os.Getenv("CW_OWNER_SEED")
	if seed == "" {
		return fmt.Errorf("--agent requires the owner seed in CW_OWNER_SEED")
	}
	o.seed = []byte(seed)
	return nil
}

// runLogin performs the grant, stores the tokens, and writes back the context +
// identity (decoded from the access token). Credentials/seed come pre-filled on
// opts (prompted or flag/env sourced by the caller).
func runLogin(ctx context.Context, o loginOpts) error {
	oc := oidc.New(o.edge)
	var tok oidc.Token
	var err error
	if o.agent {
		var tu string
		tu, err = oc.TokenEndpoint(ctx)
		if err != nil {
			return err
		}
		var assertion string
		assertion, err = identity.AgentAssertion(o.seed, o.slug, o.agentID, tu)
		if err != nil {
			return err
		}
		tok, err = oc.JWTBearerGrant(ctx, assertion)
	} else {
		tok, err = oc.PasswordGrant(ctx, o.username, o.password)
	}
	if err != nil {
		return err
	}

	claims, _ := identity.DecodeAccessClaims(tok.AccessToken)
	subject, _ := claims["sub"].(string)
	kind, _ := claims["kind"].(string)
	if kind == "" {
		if o.agent {
			kind = "agent"
		} else {
			kind = "human"
		}
	}
	display := o.username
	if o.agent {
		display = o.slug
	}

	store := tokenstore.New(o.edge, o.contextName, subject)
	if err := store.SaveRefresh(tok.RefreshToken); err != nil {
		return err
	}
	exp := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if err := store.SaveAccess(tok.AccessToken, exp); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Upsert(o.contextName, config.Context{
		Edge: o.edge,
		Identity: config.Identity{Kind: kind, Subject: subject, Display: display, Slug: o.slug},
	})
	cfg.CurrentContext = o.contextName
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Logged in to %q as %s (%s)\n", o.contextName, display, kind)
	return nil
}
```

- [ ] **Step 4: Register the auth command in `main.go`**

In `cmd/cw/main.go`, import the auth package and add to `newRootCmd` before `return root`:
```go
	root.AddCommand(auth.NewCmd(&auth.GlobalFlags{
		Context: flagContext, Edge: flagEdge, Token: flagToken, Identity: flagIdentity, JSON: flagJSON,
	}))
```
with import `"github.com/CarriedWorldUniverse/cw/internal/cli/auth"`.

> Note: cobra parses persistent flags at execute time, so pass the GlobalFlags by reading the package vars inside each RunE instead of at construction. Simpler + race-free: build `GlobalFlags` lazily. Change `NewCmd` to take a `func() *GlobalFlags` OR read the package-level flag vars directly from the auth subcommands. **Implementer:** wire it so each auth subcommand reads the CURRENT flag values at RunE time (e.g. pass pointers to the flag vars, or a getter). Verify `cw auth login --edge X --context dev` actually sees `X` (add a quick manual check or a command-level test).

- [ ] **Step 5: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/cli/auth/ -run TestLoginHuman -v`
Expected: build OK; `TestLoginHuman` PASS (context written with subject `u1`, display `alice@x`, current-context `dev`).

> The other auth subcommands (`newLogoutCmd`, `newWhoamiCmd`, `newStatusCmd`, `newSwitchCmd`, `newTokenCmd`) don't exist yet — to compile this task, add minimal stubs returning a "not implemented" error, OR implement Tasks 7–8 before building `main`. Recommended: add one-line stub constructors now (each `return &cobra.Command{Use: "...", RunE: func(...) error { return errors.New("not implemented") }}`) and replace them in Tasks 7–8.

- [ ] **Step 6: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: cw auth login (human password + agent casket assertion); register auth cmd"
```

---

## Task 7: `cw auth whoami`, `token`, `status`

**Files:**
- Create: `internal/cli/auth/whoami.go`, `internal/cli/auth/token.go`, `internal/cli/auth/status.go`
- Create: `internal/cli/auth/session.go` (shared helper to build a `*client.Client` for the resolved context)
- Create: `internal/cli/auth/whoami_test.go`

- [ ] **Step 1: Shared session helper**

`internal/cli/auth/session.go`:
```go
package auth

import (
	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// session resolves the effective context and builds a client for it. With a
// static --token, it returns a token-only client (no store).
func session(gf *GlobalFlags) (*client.Client, config.Context, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Context{}, "", err
	}
	ctx, name, err := cfg.Resolve(gf.Context, gf.Edge)
	if err != nil {
		return nil, config.Context{}, "", err
	}
	if gf.Token != "" {
		return client.WithStaticToken(ctx.Edge, gf.Token), ctx, name, nil
	}
	store := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
	return client.New(ctx.Edge, store, oidc.New(ctx.Edge)), ctx, name, nil
}
```

- [ ] **Step 2: Write the failing whoami test**

`internal/cli/auth/whoami_test.go`:
```go
package auth

import (
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/zalando/go-keyring"
)

func TestWhoamiClaims(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	// Seed a context + a non-expired cached access token with known claims.
	cfg := &config.Config{CurrentContext: "dev", Contexts: map[string]config.Context{
		"dev": {Edge: "http://edge:8080", Identity: config.Identity{Kind: "human", Subject: "u1"}},
	}}
	_ = cfg.Save()
	at := "x." + b64(`{"sub":"u1","kind":"human","org":"acme","scope":"issue:read issue:write","exp":9999999999}`) + ".y"
	_ = tokenstore.New("http://edge:8080", "dev", "u1").SaveAccess(at, time.Now().Add(time.Hour))

	info, err := whoamiInfo(&GlobalFlags{})
	if err != nil {
		t.Fatalf("whoamiInfo: %v", err)
	}
	if info.Subject != "u1" || info.Org != "acme" || info.Kind != "human" {
		t.Fatalf("info: %+v", info)
	}
	if len(info.Scopes) != 2 {
		t.Fatalf("scopes: %v", info.Scopes)
	}
}
```

- [ ] **Step 3: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/cli/auth/ -run TestWhoami`
Expected: build error (undefined `whoamiInfo`).

- [ ] **Step 4: Implement whoami / token / status**

`internal/cli/auth/whoami.go`:
```go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/spf13/cobra"
)

// Info is the resolved identity for `whoami`.
type Info struct {
	Context   string   `json:"context"`
	Subject   string   `json:"subject"`
	Kind      string   `json:"kind"`
	Org       string   `json:"org"`
	Scopes    []string `json:"scopes"`
	Products  []string `json:"products"`
	ExpiresIn int      `json:"expires_in_seconds"`
}

func whoamiInfo(gf *GlobalFlags) (Info, error) {
	c, _, name, err := session(gf)
	if err != nil {
		return Info{}, err
	}
	tok, err := c.AccessToken(context.Background()) // ensure-fresh; added below
	if err != nil {
		return Info{}, err
	}
	claims, err := identity.DecodeAccessClaims(tok)
	if err != nil {
		return Info{}, err
	}
	info := Info{Context: name}
	info.Subject, _ = claims["sub"].(string)
	info.Kind, _ = claims["kind"].(string)
	info.Org, _ = claims["org"].(string)
	if sc, _ := claims["scope"].(string); sc != "" {
		info.Scopes = strings.Fields(sc)
	}
	if prods, ok := claims["products"].([]any); ok {
		for _, p := range prods {
			if s, _ := p.(string); s != "" {
				info.Products = append(info.Products, s)
			}
		}
	}
	if exp, ok := claims["exp"].(float64); ok {
		info.ExpiresIn = int(time.Until(time.Unix(int64(exp), 0)).Seconds())
	}
	return info, nil
}

func newWhoamiCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the current identity (subject, org, scopes, products)",
		RunE: func(_ *cobra.Command, _ []string) error {
			info, err := whoamiInfo(gf)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(info)
			}
			fmt.Printf("context:  %s\nsubject:  %s\nkind:     %s\norg:      %s\nscopes:   %s\nproducts: %s\nexpires:  %ds\n",
				info.Context, info.Subject, info.Kind, info.Org,
				strings.Join(info.Scopes, " "), strings.Join(info.Products, " "), info.ExpiresIn)
			return nil
		},
	}
}
```

Add the `AccessToken` method to `internal/client/client.go` (exposes the ensure-fresh bearer for whoami/token):
```go
// AccessToken returns a currently-valid access token (silently refreshing).
func (c *Client) AccessToken(ctx context.Context) (string, error) { return c.bearer(ctx) }
```

`internal/cli/auth/token.go`:
```go
package auth

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newTokenCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print a currently-valid access token (auto-refreshing) for scripting",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, _, _, err := session(gf)
			if err != nil {
				return err
			}
			tok, err := c.AccessToken(context.Background())
			if err != nil {
				return err
			}
			fmt.Println(tok)
			return nil
		},
	}
}
```

`internal/cli/auth/status.go`:
```go
package auth

import (
	"fmt"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

func newStatusCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List contexts and token freshness",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Contexts) == 0 {
				fmt.Println("no contexts (run 'cw auth login --edge <url>')")
				return nil
			}
			for name, ctx := range cfg.Contexts {
				marker := "  "
				if name == cfg.CurrentContext {
					marker = "* "
				}
				state := "logged-out"
				st := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
				if _, exp, err := st.Access(); err == nil {
					if time.Until(exp) > 0 {
						state = "valid"
					} else if _, rerr := st.Refresh(); rerr == nil {
						state = "refreshable"
					}
				} else if _, rerr := st.Refresh(); rerr == nil {
					state = "refreshable"
				}
				fmt.Printf("%s%-12s %-28s %s (%s)\n", marker, name, ctx.Edge, ctx.Identity.Display, state)
			}
			return nil
		},
	}
}
```

- [ ] **Step 5: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/cli/auth/ -run TestWhoami -v`
Expected: build OK; `TestWhoamiClaims` PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: cw auth whoami/token/status + client.AccessToken ensure-fresh accessor"
```

---

## Task 8: `cw auth logout`, `switch`

**Files:**
- Create: `internal/cli/auth/logout.go`, `internal/cli/auth/switch.go`, `internal/cli/auth/logout_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/auth/logout_test.go`:
```go
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
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/cli/auth/ -run TestLogout`
Expected: build error (undefined `runLogout`).

- [ ] **Step 3: Implement logout + switch**

`internal/cli/auth/logout.go`:
```go
package auth

import (
	"context"
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

func newLogoutCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the refresh token and clear stored credentials for the context",
		RunE:  func(_ *cobra.Command, _ []string) error { return runLogout(gf) },
	}
}

func runLogout(gf *GlobalFlags) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, name, err := cfg.Resolve(gf.Context, gf.Edge)
	if err != nil {
		return err
	}
	store := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
	// Best-effort revoke at herald before wiping locally.
	if rtok, rerr := store.Refresh(); rerr == nil {
		if verr := oidc.New(ctx.Edge).Revoke(context.Background(), rtok); verr != nil {
			fmt.Fprintf(os.Stderr, "cw: revoke warning: %v\n", verr)
		}
	}
	if err := store.Clear(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Logged out of %q\n", name)
	return nil
}
```

`internal/cli/auth/switch.go`:
```go
package auth

import (
	"fmt"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/spf13/cobra"
)

func newSwitchCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <context>",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Contexts[args[0]]; !ok {
				return fmt.Errorf("no such context %q", args[0])
			}
			cfg.CurrentContext = args[0]
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("switched to %q\n", args[0])
			return nil
		},
	}
}
```

> Replace the Task-6 stub constructors for logout/switch (and whoami/token/status from Task 7) with these real ones if you stubbed them.

- [ ] **Step 4: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go test ./... -v`
Expected: build OK; ALL package tests PASS (config, tokenstore, oidc, identity, client, cli/auth).

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: cw auth logout (revoke + clear) and switch"
```

---

## Task 9: README + live integration smoke (gated) + GitHub repo

**Files:**
- Create: `README.md`, `internal/cli/auth/integration_test.go`

- [ ] **Step 1: README**

`README.md`:
```markdown
# cw — the CWB platform CLI

One binary for humans and agents. Anchored on a single edge URL (the interchange
gateway); authenticates against herald and keeps the session fresh.

## Auth

    cw auth login --edge https://cwb.example --context prod    # human: prompts email + password
    cw auth login --agent --agent-id <id> --slug shadow        # agent: CW_OWNER_SEED in env
    cw auth whoami
    cw auth token            # print a fresh access token (scripting)
    cw auth status           # list contexts + freshness
    cw auth switch prod
    cw auth logout

A *context* is `{edge, identity}`. The refresh token is stored in the OS
keychain; the access token is cached (0600) and silently refreshed. Use
`--token`/`CW_TOKEN` to present a bearer directly (no stored state).

Command groups for cairn (`repo`/`pr`), ledger (`issue`), commonplace (`kb`),
and herald admin (`org`) build on this core and ship separately.
```

- [ ] **Step 2: Gated live integration test**

`internal/cli/auth/integration_test.go`:
```go
package auth

import (
	"context"
	"os"
	"testing"
)

// TestLiveLogin exercises the full login→whoami→logout loop against a real
// deployment. Gated: set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD to run.
//   CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//   CW_IT_USER=cwadmin@carriedworld.com CW_IT_PASSWORD=... go test ./internal/cli/auth/ -run TestLiveLogin -v
func TestLiveLogin(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD to run the live login test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	if err := runLogin(context.Background(), loginOpts{
		edge: edge, contextName: "it", username: os.Getenv("CW_IT_USER"), password: os.Getenv("CW_IT_PASSWORD"),
	}); err != nil {
		t.Fatalf("live login: %v", err)
	}
	info, err := whoamiInfo(&GlobalFlags{Context: "it"})
	if err != nil || info.Subject == "" {
		t.Fatalf("whoami: %v %+v", err, info)
	}
	if err := runLogout(&GlobalFlags{Context: "it"}); err != nil {
		t.Fatalf("logout: %v", err)
	}
}
```

> NOTE: the live test uses the OS keychain. On a headless CI box `go-keyring` needs the file fallback or a Secret Service; the controller runs this on dMon (where the deploy lives) or locally, not in unattended CI. It is `t.Skip`-gated so the normal suite stays green everywhere.

- [ ] **Step 3: Run the gated test locally / from dMon**

Controller step (not the implementer): with herald live on dMon, run
```bash
CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
CW_IT_USER=cwadmin@carriedworld.com \
CW_IT_PASSWORD=<genesis_owner_password> \
  go test ./internal/cli/auth/ -run TestLiveLogin -v
```
Expected: PASS (login as cwadmin → whoami shows the platform-admin subject → logout revokes).

- [ ] **Step 4: Full offline suite + commit**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go vet ./... && go test ./...`
Expected: all green (the live test SKIPs).
```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: README + gated live login integration test"
```

- [ ] **Step 5: Create the GitHub repo + push (controller)**

```bash
cd /Users/jacinta/Source/cw
gh repo create CarriedWorldUniverse/cw --public --source=. --remote=origin --description "CWB platform CLI (cw) — human + agent client"
git push -u origin main   # or open a PR if main is protected after creation
```

---

## Self-review

**Spec coverage (Part B of the design):**
- Single edge anchor; context = {edge, identity}; product routes derived from edge → `config` + `client.URL`. ✔
- Token store: refresh in keychain, access cached 0600, keyed {edge,subject} → Task 2. ✔
- Silent refresh + 401 retry; `ErrReauth` fallback → Task 5 (`client`). ✔
- Human password grant + agent casket assertion; claims decoded for identity → Tasks 4, 6. ✔
- `--token`/`CW_TOKEN` stateless path → `client.WithStaticToken` + `session`. ✔
- Agent seed from env (`CW_OWNER_SEED`), id/slug from flag/env → Task 6 `loadAgentIdentity`. (The identity-file source named in the spec is deferred to a later refinement — flagged in Task 6; env+flags cover the ToolRunner path. This is the one intentional spec-narrowing; note it.) ✔ (narrowed)
- `cw auth login/logout/whoami/status/switch/token` → Tasks 6–8. ✔
- Discovery-driven token/revocation endpoints → Task 3. ✔
- Logout revokes + wipes → Task 8. ✔
- Tech: Go, cobra, go-keyring, casket-go, go-jose, x/term, yaml → Task 1 deps. ✔

**Placeholder scan:** No TBD/TODO. Every code step has complete code; tests have real assertions. Two implementer-guidance notes (the cobra flag-timing wiring in Task 6 Step 4, and the Task-6 stub-then-replace ordering) are explicit instructions, not placeholders.

**Type consistency:** `config.Context`/`config.Identity`/`Config.Resolve`/`Current`/`Upsert`; `tokenstore.New(edge,context,subject)` + `SaveRefresh`/`Refresh`/`SaveAccess`/`Access`/`Clear`; `oidc.New(edge)` + `Discover`/`PasswordGrant`/`JWTBearerGrant`/`RefreshGrant`/`Revoke`/`TokenEndpoint` + `oidc.Token{AccessToken,RefreshToken,ExpiresIn}`; `identity.AgentAssertion`/`DecodeAccessClaims`/`PromptHuman`; `client.New`/`WithStaticToken`/`Do`/`Get`/`URL`/`AccessToken` + `ErrReauth`; `auth.GlobalFlags`/`NewCmd`/`session`/`runLogin`/`whoamiInfo`/`runLogout`. Used consistently across tasks. ✔

**One known integration risk flagged for the implementer:** cobra persistent-flag values are only populated at Execute time, so `auth.NewCmd` must read the flag vars lazily (Task 6 Step 4 note). The implementer must verify `--edge`/`--context` reach `runLogin` (a command-level test or manual check), not just the unit tests that call `runLogin` directly.
