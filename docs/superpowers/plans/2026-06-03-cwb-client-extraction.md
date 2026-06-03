# `cwb-client` extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Two-repo cycle: build the `cwb-client` module (Tasks 1–2), then repoint `cw` to it (Task 3). Cross-repo merge/pin is a CONTROLLER step between Task 2 and Task 3.

**Goal:** Extract cw's auth/HTTP/pillar client into a standalone `github.com/CarriedWorldUniverse/cwb-client` module, with `client` inverted onto a `TokenSource` interface, so nexus/outposts/aspects can import the proven code.

**Architecture:** Move `client`(inverted)/`oidc`/`identity`(crypto)/`herald`/`ledger`/`cairn`/`commonplace` into the new module. cw keeps `config`/`tokenstore`/the prompts, provides a tokenstore-backed `TokenSource`, and repoints its imports. Pure refactor — no wire-behavior change.

**Tech:** Go 1.26. Spec: `cw/docs/superpowers/specs/2026-06-03-cwb-client-extraction-design.md`.

## Verified facts

- `internal/client/client.go`: `New(edge, store, oc)`, `WithStaticToken(edge, token)`, `bearer`/`refresh` (store.Access→fresh?→refresh; refresh = store.Refresh→oidc.RefreshGrant→SaveRefresh+SaveAccess; `ErrReauth` on failure), `Do` (401 → refresh+retry, but `&& staticToken==""` so a **static-token 401 returns the bare response**), `AccessToken`/`URL`/`Get`/`do`. Imports `oidc`+`tokenstore`.
- `internal/cmdutil/cmdutil.go:38,41`: Session does `client.WithStaticToken(ctx.Edge, gf.Token)` (static) / `client.New(ctx.Edge, store, oidc.New(ctx.Edge))` (store). cmdutil already imports client/config/oidc/tokenstore.
- `internal/identity/identity.go`: `AgentAssertion`/`AgentAssertionAt`/`DecodeAccessClaims`/`DecodeClaimsBytes` (casket-go+go-jose) + `PromptHuman`/`PromptPassword` (golang.org/x/term); `fingerprint.go`: `Fingerprint`.
- Repoint surface in cw: client 7 files, oidc 4, herald 4, ledger 1, cairn 2, commonplace 1, identity 5. All import-path rewrites.
- tokenstore: `New`,`SaveRefresh`,`Refresh`,`SaveAccess`,`Access`,`Clear`. oidc: `New`,`Discover`,`PasswordGrant`,`JWTBearerGrant`,`RefreshGrant`,`TokenEndpoint`,`Revoke`.
- Lib module path: `github.com/CarriedWorldUniverse/cwb-client`; package import paths `cwb-client/{client,oidc,identity,herald,ledger,cairn,commonplace}`. Moved files keep their `package X` decl; only internal import paths change (`.../cw/internal/X` → `.../cwb-client/X`).

---

## Task 1: `cwb-client` module — core (`client` inverted, `oidc`, `identity` crypto)

**Repo:** `/Users/jacinta/Source/cwb-client` (new; create the dir + `git init` + branch `main` work on `nex-extract`)
**Files:** `go.mod`, `client/client.go`(+test), `oidc/oidc.go`(+test), `identity/identity.go`+`identity/fingerprint.go`(+tests)

- [ ] **Step 1: Scaffold the module**

```bash
mkdir -p /Users/jacinta/Source/cwb-client && cd /Users/jacinta/Source/cwb-client && git init -q && git checkout -q -b nex-extract
cat > go.mod <<'EOF'
module github.com/CarriedWorldUniverse/cwb-client

go 1.26

require (
	github.com/CarriedWorldUniverse/casket-go v0.1.0
	github.com/go-jose/go-jose/v4 v4.0.5
)
EOF
```
(Match the exact `casket-go`/`go-jose` versions from `cw/go.mod` — read them and correct the lines if they differ. Run `go mod tidy` at the end of the task to populate go.sum.)

- [ ] **Step 2: `client/client.go` — the inverted seam**

Copy `cw/internal/client/client.go`, then replace the token machinery with the `TokenSource` interface (drop the `oidc`/`tokenstore`/`go-keyring` imports entirely):

```go
// Package client is the edge-anchored HTTP seam. It presents a bearer from a
// TokenSource to product routes under the edge (<edge>/<pillar>/<path>), and on
// a 401 asks the source to refresh and retries once.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrReauth means re-auth is required (the refresh path is exhausted).
var ErrReauth = errors.New("session expired: run 'cw auth login'")

// ErrNoRefresh is returned by a TokenSource that cannot refresh (e.g. a static
// token); on a 401 the client surfaces the original response instead of retrying.
var ErrNoRefresh = errors.New("token source cannot refresh")

// TokenSource supplies (and refreshes) the bearer the client presents.
type TokenSource interface {
	Token(ctx context.Context) (string, error)   // current token, refreshing if stale
	Refresh(ctx context.Context) (string, error) // force-refresh after a 401; ErrReauth/ErrNoRefresh
}

// Client targets one edge as one identity (its TokenSource).
type Client struct {
	edge string
	src  TokenSource
	hc   *http.Client
}

// New builds a Client from an edge + a TokenSource.
func New(edge string, src TokenSource) *Client {
	return &Client{edge: strings.TrimRight(edge, "/"), src: src, hc: &http.Client{Timeout: 30 * time.Second}}
}

// staticSource always returns a fixed token and cannot refresh.
type staticSource struct{ token string }

func (s staticSource) Token(context.Context) (string, error)   { return s.token, nil }
func (s staticSource) Refresh(context.Context) (string, error) { return "", ErrNoRefresh }

// WithStaticToken returns a Client that always uses the given bearer (stateless
// per-invocation use, e.g. --token / ToolRunner agents).
func WithStaticToken(edge, token string) *Client { return New(edge, staticSource{token}) }

// AccessToken returns a currently-valid access token (refreshing if stale).
func (c *Client) AccessToken(ctx context.Context) (string, error) { return c.src.Token(ctx) }

// URL builds a product URL: <edge>/<pillar><path> (path begins with "/").
func (c *Client) URL(pillar, path string) string {
	return c.edge + "/" + strings.Trim(pillar, "/") + path
}

// Do executes an authenticated request; on a 401 it asks the source to refresh
// and retries once (ErrNoRefresh → surface the original response). body is []byte
// so the retry can resend it.
func (c *Client) Do(ctx context.Context, method, pillar, path string, body []byte) (*http.Response, []byte, error) {
	tok, err := c.src.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, raw, err := c.do(ctx, method, c.URL(pillar, path), tok, body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		fresh, rerr := c.src.Refresh(ctx)
		if errors.Is(rerr, ErrNoRefresh) {
			return resp, raw, nil
		}
		if rerr != nil {
			return nil, nil, rerr
		}
		return c.do(ctx, method, c.URL(pillar, path), fresh, body)
	}
	return resp, raw, nil
}

// Get is a convenience wrapper.
func (c *Client) Get(ctx context.Context, pillar, path string) (*http.Response, []byte, error) {
	return c.Do(ctx, http.MethodGet, pillar, path, nil)
}

func (c *Client) do(ctx context.Context, method, url, bearer string, body []byte) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err == nil && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("client: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("client: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("client: %s %s: read body: %w", method, url, err)
	}
	return resp, raw, nil
}
```

`client/client_test.go`: a stub-server test covering (a) `WithStaticToken` happy path, (b) a 401 with the static source surfaces the bare 401 (not ErrReauth), (c) a fake refreshing `TokenSource` whose `Refresh` returns a new token → the 401 retry succeeds, (d) a `TokenSource` whose `Refresh` returns `ErrReauth` → `Do` returns `ErrReauth`.

- [ ] **Step 3: Move `oidc` + `identity` (crypto split)**

- Copy `cw/internal/oidc/*.go` → `cwb-client/oidc/` unchanged (it has no internal deps; package decl stays `package oidc`).
- Copy `cw/internal/identity/identity.go` → `cwb-client/identity/identity.go` but **remove `PromptHuman` + `PromptPassword`** (and their `bufio`/`os`/`strings`/`golang.org/x/term` imports if now unused) — keep `AgentAssertion`/`AgentAssertionAt`/`DecodeAccessClaims`/`DecodeClaimsBytes`. Copy `cw/internal/identity/fingerprint.go` → `cwb-client/identity/fingerprint.go` unchanged. Copy the relevant tests (`identity_test.go`, `fingerprint_test.go`); drop any prompt tests.
- Within all moved files, no internal import rewrites are needed yet for oidc/identity (they import no cw-internal packages).

- [ ] **Step 4: Build + test the core**

Run: `cd /Users/jacinta/Source/cwb-client && go mod tidy && go build ./... && go test ./... && go vet ./...`
Expected: `client`, `oidc`, `identity` build + tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cwb-client && git add -A
git commit -m "cwb-client: module scaffold + client (TokenSource-inverted), oidc, identity (crypto)"
```

---

## Task 2: `cwb-client` — the four pillar wrappers

**Repo:** `/Users/jacinta/Source/cwb-client` (branch `nex-extract`)
**Files:** `herald/`, `ledger/`, `cairn/`, `commonplace/` (each `.go` + `_test.go`)

- [ ] **Step 1: Move the wrappers**

For each of `herald`, `ledger`, `cairn`, `commonplace`: copy `cw/internal/<p>/*.go` → `cwb-client/<p>/`, and rewrite the one internal import `github.com/CarriedWorldUniverse/cw/internal/client` → `github.com/CarriedWorldUniverse/cwb-client/client` in both the package file and its test (the tests use `client.WithStaticToken`). Package decls stay `package <p>`.

- [ ] **Step 2: Build + test the full lib**

Run: `cd /Users/jacinta/Source/cwb-client && go build ./... && go test ./... && go vet ./...`
Expected: all seven packages build + their ported unit tests pass (the httptest-stub wrapper tests are CLI-dep-free).

- [ ] **Step 3: Commit**

```bash
cd /Users/jacinta/Source/cwb-client && git add -A
git commit -m "cwb-client: herald/ledger/cairn/commonplace pillar wrappers"
```

> **CONTROLLER (between Task 2 and Task 3):** create the GitHub repo `CarriedWorldUniverse/cwb-client`, push `nex-extract`, open + **merge** to `main` (squash), capture the merged-main hash `H` (`git -C ../cwb-client rev-parse --short origin/main`). Provide `H` to Task 3. (Optionally add a minimal go-test CI workflow.)

---

## Task 3: repoint `cw` onto `cwb-client`

**Repo:** `/Users/jacinta/Source/cw` (branch `nex-cwb-client`)
**Files:** `go.mod`/`go.sum`; new `internal/prompt/prompt.go`; `internal/cmdutil/cmdutil.go` (+ a `tokensource.go`); repoint `internal/cli/*`; delete the moved `internal/` packages.

This must be done **atomically** — the repo won't compile until every import is repointed.

- [ ] **Step 1: Add the dep** — `cd /Users/jacinta/Source/cw && go get github.com/CarriedWorldUniverse/cwb-client@<H>`

- [ ] **Step 2: Move the prompts to `internal/prompt/prompt.go`**

Create `internal/prompt/prompt.go` (`package prompt`) holding `PromptHuman` + `PromptPassword` verbatim from the old `internal/identity/identity.go` (with their `bufio`/`errors`/`fmt`/`io`/`os`/`strings`/`golang.org/x/term` imports). These are the only identity bits that stay in cw.

- [ ] **Step 3: Add cw's `TokenSource` (the relocated refresh logic)** — `internal/cmdutil/tokensource.go`:

```go
package cmdutil

import (
	"context"
	"time"

	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

const skew = 60 * time.Second

// storeSource is cw's tokenstore+oidc-backed TokenSource (silent refresh) — the
// refresh policy relocated out of the client seam.
type storeSource struct {
	store *tokenstore.Store
	oc    *oidc.Client
}

func (s *storeSource) Token(ctx context.Context) (string, error) {
	tok, exp, err := s.store.Access()
	if err == nil && time.Until(exp) > skew {
		return tok, nil
	}
	return s.Refresh(ctx)
}

func (s *storeSource) Refresh(ctx context.Context) (string, error) {
	rtok, err := s.store.Refresh()
	if err != nil {
		return "", client.ErrReauth
	}
	t, err := s.oc.RefreshGrant(ctx, rtok)
	if err != nil {
		return "", client.ErrReauth
	}
	if err := s.store.SaveRefresh(t.RefreshToken); err != nil {
		return "", err
	}
	if err := s.store.SaveAccess(t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second)); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}
```

- [ ] **Step 4: Update `Session`** in `internal/cmdutil/cmdutil.go`

Change the store-path line (`client.New(ctx.Edge, store, oidc.New(ctx.Edge))`) to:
```go
	return client.New(ctx.Edge, &storeSource{store: store, oc: oidc.New(ctx.Edge)}), ctx, name, nil
```
The static-path line stays `client.WithStaticToken(ctx.Edge, gf.Token)`. Repoint cmdutil's imports of `client`/`oidc` to the `cwb-client/...` paths (tokenstore/config stay cw-internal).

- [ ] **Step 5: Repoint every import** — across `internal/cli/*` (and any other non-test + test files), rewrite:
```
github.com/CarriedWorldUniverse/cw/internal/client       → github.com/CarriedWorldUniverse/cwb-client/client
github.com/CarriedWorldUniverse/cw/internal/oidc         → github.com/CarriedWorldUniverse/cwb-client/oidc
github.com/CarriedWorldUniverse/cw/internal/herald       → github.com/CarriedWorldUniverse/cwb-client/herald
github.com/CarriedWorldUniverse/cw/internal/ledger       → github.com/CarriedWorldUniverse/cwb-client/ledger
github.com/CarriedWorldUniverse/cw/internal/cairn        → github.com/CarriedWorldUniverse/cwb-client/cairn
github.com/CarriedWorldUniverse/cw/internal/commonplace  → github.com/CarriedWorldUniverse/cwb-client/commonplace
```
And for `internal/identity`: the **crypto** uses (`AgentAssertion`/`AgentAssertionAt`/`DecodeAccessClaims`/`Fingerprint`) repoint to `cwb-client/identity`; the **prompt** uses (`PromptHuman`/`PromptPassword`) repoint to `cw/internal/prompt`. Files touched: `internal/cli/auth/login.go` (AgentAssertion + PromptHuman), `internal/cli/auth/whoami.go` (DecodeAccessClaims), `internal/cli/agent/agent.go` (Fingerprint), `internal/cli/human/human.go` (PromptPassword), + their tests if they reference these. (`go build`/`go vet` will pinpoint any missed reference.)

- [ ] **Step 6: Delete the moved internal packages**

```bash
cd /Users/jacinta/Source/cw && rm -rf internal/client internal/oidc internal/identity internal/herald internal/ledger internal/cairn internal/commonplace
```

- [ ] **Step 7: Build + test + vet** — `cd /Users/jacinta/Source/cw && go mod tidy && go build ./... && go test ./... && go vet ./...`
Expected: full suite green — the existing cli/* wiring tests + (re-imported) wrapper tests are the regression guard. Fix any dangling reference the compiler flags. `go run ./cmd/cw --help` lists all groups unchanged.

- [ ] **Step 8: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: consume cwb-client; tokenstore-backed TokenSource; prompts → internal/prompt"
```

- [ ] **Step 9: Controller — live smoke + merge.** With the dMon edge: `cw auth login --agent` (a provisioned agent — refresh + assertion through the new lib) + a pillar op (`cw whoami --remote` and/or `cw kb list`) prove no behavior drift. Then PR + merge cw. (cwb-client already merged.)

---

## Self-review

**Spec coverage:**
- new `cwb-client` module with `client`(inverted)/`oidc`/`identity`(crypto)/`herald`/`ledger`/`cairn`/`commonplace` → Tasks 1–2. ✔
- `TokenSource` inversion (+ `ErrNoRefresh` preserving the static-token bare-401) → Task 1 client.go + test. ✔
- `identity` split (prompts stay in cw) → Task 1 (lib) + Task 3 Step 2 (cw `internal/prompt`). ✔
- cw provides the tokenstore-backed `TokenSource`, repoints, deletes the moved packages → Task 3. ✔
- pure refactor / no behavior change, regression-guarded by cw's existing suite + a live smoke → Task 3 Steps 7,9. ✔

**Placeholder scan:** the only judgment spots are matching the exact casket-go/go-jose versions (read cw/go.mod) and catching any import reference the compiler flags — both self-correcting via `go build`.

**Type consistency:** `client.TokenSource`{Token,Refresh}; `client.New(edge, TokenSource)`; `client.WithStaticToken` (lib, via `staticSource`); `client.ErrReauth`/`ErrNoRefresh`; cw `storeSource` implements `TokenSource` using `tokenstore`+`oidc.RefreshGrant`. Wrapper/identity/oidc public APIs unchanged — only import paths move.
