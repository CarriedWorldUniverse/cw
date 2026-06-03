# `cw whoami --remote` Implementation Plan (#7b)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `internal/herald.Me()` + a `--remote` flag on `cw whoami` that renders herald's server-authoritative identity record (`GET /api/me`, live on dMon since #7a).

**Architecture:** cw-only. `internal/herald` gains the wrapper; `cw whoami` (in `internal/cli/auth`) gains `--remote`; default whoami unchanged. No import cycle (`auth → herald → client`).

**Tech Stack:** Go 1.26, cobra. No new deps.

Sub-project **#7b**. Spec: `docs/superpowers/specs/2026-06-03-cw-whoami-remote-design.md`. Endpoint shape (verified live): bare `UserInfo{id,kind,display_name,org,org_name,status,scopes[],responsible_human,fingerprint}` (snake_case; agent fields empty for humans); 401 unauth.

---

## Task 1: `internal/herald` — `UserInfo` + `Me`

**Files:**
- Modify: `internal/herald/herald.go` (add `UserInfo` + `Me`)
- Modify: `internal/herald/herald_test.go` (add `TestMe`)

- [ ] **Step 1: Write the failing test** — append to `internal/herald/herald_test.go` (reuses the file's `decode` helper + `client`/`context`/`http`/`httptest`/`testing`/`strings` imports):

```go
func TestMe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, r *http.Request) {
		// Return a human record (empty agent fields) the first call, an agent the second.
		if r.Header.Get("X-Want") == "agent" {
			_, _ = w.Write([]byte(`{"id":"a1","kind":"agent","display_name":"builder","org":"o1","org_name":"acme","status":"active","scopes":["repo:read"],"responsible_human":"h1","fingerprint":"SHA256:zzz"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"h1","kind":"human","display_name":"alice@x","org":"o1","org_name":"acme","status":"active","scopes":["issue:read"]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.WithStaticToken(srv.URL, "tok")

	hu, err := Me(context.Background(), c)
	if err != nil || hu.ID != "h1" || hu.Kind != "human" || hu.OrgName != "acme" || hu.Status != "active" {
		t.Fatalf("Me(human): %v %+v", err, hu)
	}
	if hu.ResponsibleHuman != "" || hu.Fingerprint != "" {
		t.Fatalf("human should have no agent fields: %+v", hu)
	}
	if len(hu.Scopes) != 1 || hu.Scopes[0] != "issue:read" {
		t.Fatalf("human scopes: %v", hu.Scopes)
	}
}

func TestMeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing identity"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.WithStaticToken(srv.URL, "tok")
	if _, err := Me(context.Background(), c); err == nil || !strings.Contains(err.Error(), "missing identity") {
		t.Fatalf("Me error: want server message, got %v", err)
	}
}
```

> The agent branch keys on an `X-Want` header only to keep a single stub; the wrapper itself doesn't send it — `TestMe` exercises the human shape. (The agent shape is covered live + in the cw command wiring test.) If you prefer, split into two servers; either is fine.

- [ ] **Step 2: Run — expect FAIL** — `cd /Users/jacinta/Source/cw && go test ./internal/herald/ -run TestMe`
Expected: build error (undefined `UserInfo`/`Me`).

- [ ] **Step 3: Implement** — add to `internal/herald/herald.go`:

```go
// UserInfo is the caller's own authoritative identity from GET /api/me (agent
// fields empty for humans).
type UserInfo struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	DisplayName      string   `json:"display_name"`
	Org              string   `json:"org"`
	OrgName          string   `json:"org_name"`
	Status           string   `json:"status"`
	Scopes           []string `json:"scopes"`
	ResponsibleHuman string   `json:"responsible_human"`
	Fingerprint      string   `json:"fingerprint"`
}

// Me returns the caller's own authoritative identity record (server-side).
func Me(ctx context.Context, c *client.Client) (UserInfo, error) {
	var ui UserInfo
	err := do(ctx, c, http.MethodGet, "/api/me", nil, &ui)
	return ui, err
}
```

- [ ] **Step 4: Run — expect PASS** — `cd /Users/jacinta/Source/cw && go test ./internal/herald/ -v && go build ./... && go vet ./internal/herald/`
Expected: `TestMe`/`TestMeError` (+ existing) PASS; build + vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/herald/
git commit -m "herald: add Me (GET /api/me) wrapper + UserInfo"
```

---

## Task 2: `cw whoami --remote`

**Files:**
- Modify: `internal/cli/auth/whoami.go` (add `--remote` + `remoteWhoami`)
- Modify: `internal/cli/auth/whoami_test.go` (wiring tests)

- [ ] **Step 1: Write the failing wiring test** — append to `internal/cli/auth/whoami_test.go`:

```go
func TestWhoamiRemote(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hit string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, r *http.Request) {
		hit = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"a1","kind":"agent","display_name":"builder","org":"o1","org_name":"acme","status":"active","scopes":["repo:read","repo:write"],"responsible_human":"h1","fingerprint":"SHA256:zzz"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewWhoamiCmd(gf)
	cmd.SetArgs([]string{"--remote"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("whoami --remote: %v", err)
	}
	if hit != "/herald/api/me" {
		t.Fatalf("endpoint not hit: %q", hit)
	}
	s := out.String()
	for _, want := range []string{"agent", "builder", "acme", "active", "repo:write", "h1", "SHA256:zzz"} {
		if !strings.Contains(s, want) {
			t.Fatalf("--remote output missing %q:\n%s", want, s)
		}
	}
}

func TestWhoamiRemoteJSON(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"h1","kind":"human","display_name":"alice@x","org":"o1","org_name":"acme","status":"active","scopes":["issue:read"]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &GlobalFlags{Edge: srv.URL, Token: "tok", JSON: true}
	cmd := NewWhoamiCmd(gf)
	cmd.SetArgs([]string{"--remote"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("whoami --remote --json: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if got["id"] != "h1" || got["org_name"] != "acme" {
		t.Fatalf("unexpected --json: %v", got)
	}
}
```

Add the needed imports to the test file if missing: `bytes`, `encoding/json`, `net/http`, `net/http/httptest`, `strings`.

- [ ] **Step 2: Run — expect FAIL** — `cd /Users/jacinta/Source/cw && go test ./internal/cli/auth/ -run TestWhoamiRemote`
Expected: FAIL — `--remote` flag unknown (cobra errors on the arg).

- [ ] **Step 3: Implement** — in `internal/cli/auth/whoami.go`, add the `herald` import and rework `NewWhoamiCmd` to add `--remote` + a `remoteWhoami` renderer. Replace the whole `NewWhoamiCmd` func with:

```go
// NewWhoamiCmd builds the whoami command, registered both at the top level
// (`cw whoami`) and under `cw auth` (the alias). --remote fetches the
// server-authoritative record from herald's GET /api/me.
func NewWhoamiCmd(gf *GlobalFlags) *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the current identity (local; --remote for the server-authoritative record)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if remote {
				return remoteWhoami(cmd, gf)
			}
			info, err := whoamiInfo(gf)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if gf.JSON {
				return json.NewEncoder(out).Encode(info)
			}
			expires := fmt.Sprintf("%ds", info.ExpiresIn)
			if info.ExpiresIn <= 0 {
				expires = "expired"
			}
			fmt.Fprintf(out, "context:  %s\nedge:     %s\nkind:     %s\nsubject:  %s\n",
				info.Context, info.Edge, info.Kind, info.Subject)
			if info.Display != "" {
				fmt.Fprintf(out, "display:  %s\n", info.Display)
			}
			if info.Slug != "" {
				fmt.Fprintf(out, "slug:     %s\n", info.Slug)
			}
			fmt.Fprintf(out, "org:      %s\nscopes:   %s\nproducts: %s\nexpires:  %s\n",
				info.Org, strings.Join(info.Scopes, " "), strings.Join(info.Products, " "), expires)
			return nil
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "fetch the server-authoritative record from herald (GET /api/me)")
	return cmd
}

// remoteWhoami renders herald's authoritative identity record (status, org name,
// server scopes, + agent responsible_human/fingerprint).
func remoteWhoami(cmd *cobra.Command, gf *GlobalFlags) error {
	c, sess, name, err := session(gf)
	if err != nil {
		return err
	}
	ui, err := herald.Me(cmd.Context(), c)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if gf.JSON {
		return json.NewEncoder(out).Encode(ui)
	}
	fmt.Fprintf(out, "context:  %s\nedge:     %s\nid:       %s\nkind:     %s\n", name, sess.Edge, ui.ID, ui.Kind)
	if ui.DisplayName != "" {
		fmt.Fprintf(out, "display:  %s\n", ui.DisplayName)
	}
	org := ui.Org
	if ui.OrgName != "" {
		org = fmt.Sprintf("%s  (%s)", ui.Org, ui.OrgName)
	}
	fmt.Fprintf(out, "org:      %s\nstatus:   %s\nscopes:   %s\n", org, ui.Status, strings.Join(ui.Scopes, " "))
	if ui.ResponsibleHuman != "" {
		fmt.Fprintf(out, "responsible_human: %s\n", ui.ResponsibleHuman)
	}
	if ui.Fingerprint != "" {
		fmt.Fprintf(out, "fingerprint:       %s\n", ui.Fingerprint)
	}
	return nil
}
```

Add the import `"github.com/CarriedWorldUniverse/cw/internal/herald"` to `whoami.go`.

> Note: the default-path printer is switched from `os.Stdout` to `cmd.OutOrStdout()` (so both paths render through the cobra writer and the tests capture output). Behavior is identical at runtime (defaults to os.Stdout); the existing #6 tests call `whoamiInfo` directly so they're unaffected. `os` may become an unused import in whoami.go after this change — remove it if so (the default path no longer references `os.Stdout`; `whoamiInfo` doesn't use `os`). Verify with `go build`.

- [ ] **Step 4: Run — expect PASS** — `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/cli/auth/ -v && go test ./... && go vet ./...`
Expected: build OK (no unused-import error); `TestWhoamiRemote`/`TestWhoamiRemoteJSON` + the existing whoami tests PASS; full suite green. `go run ./cmd/cw whoami --help` shows `--remote`; `go run ./cmd/cw auth whoami --help` too (same factory).

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/cli/auth/
git commit -m "cw whoami: --remote (server-authoritative record via herald GET /api/me)"
```

---

## Task 3: README + gated live integration

**Files:**
- Modify: `README.md`
- Create: `internal/cli/auth/whoami_remote_integration_test.go` (gated)

- [ ] **Step 1: README** — under the existing `## Identity` section, add the `--remote` line:

```markdown
    cw whoami --remote   # server-authoritative record (status, org name, live scopes, agent responsible-human + fingerprint) via herald GET /api/me
```
and a sentence: ```--remote` calls herald (`GET /api/me`) for the authoritative record — the only whoami path that makes a network call; it also proves the token works server-side.``

- [ ] **Step 2: Gated live integration** — `internal/cli/auth/whoami_remote_integration_test.go` (package `auth`):

```go
package auth

import (
	"context"
	"os"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
)

// TestLiveWhoamiRemote logs in a provisioned agent and asserts herald.Me returns
// the authoritative agent record (kind=agent, responsible_human + fingerprint,
// status=active). Gated on CW_IT_EDGE + CW_IT_AGENT_* (set by the controller's
// live run, which provisions an agent via cw and exports its login material).
//
// Required env: CW_IT_EDGE, CW_IT_OWNER_SEED, CW_IT_AGENT_ID, CW_IT_AGENT_SLUG.
func TestLiveWhoamiRemote(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" || os.Getenv("CW_IT_AGENT_ID") == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_OWNER_SEED + CW_IT_AGENT_ID + CW_IT_AGENT_SLUG to run the live remote-whoami test")
	}
	ctx := context.Background()
	// Log the agent in via the same assertion path cw auth login --agent uses.
	c, err := liveAgentClient(t, edge,
		os.Getenv("CW_IT_OWNER_SEED"), os.Getenv("CW_IT_AGENT_SLUG"), os.Getenv("CW_IT_AGENT_ID"))
	if err != nil {
		t.Fatalf("agent login: %v", err)
	}
	ui, err := herald.Me(ctx, c)
	if err != nil {
		t.Fatalf("herald.Me: %v", err)
	}
	if ui.Kind != "agent" || ui.ResponsibleHuman == "" || ui.Fingerprint == "" || ui.Status != "active" {
		t.Fatalf("agent UserInfo: %+v", ui)
	}
	_ = oidc.New // keep the import if liveAgentClient lives elsewhere; remove if unused
}
```

> **Implementer note:** implement a small `liveAgentClient(t, edge, seed, slug, agentID) (*client.Client, error)` helper in this test file that mirrors `internal/cli/auth/login.go`'s agent path: `oidc.New(edge)` → `TokenEndpoint` → `identity.AgentAssertion([]byte(seed), slug, agentID, tokenURL)` → `JWTBearerGrant` → `client.WithStaticToken(edge, tok.AccessToken)`. (Reuse the real symbols; drop the `oidc.New` keep-alive line and import only what you use.) This avoids needing a stored context — the agent logs in fresh and `Me` is called with the resulting bearer. If a sibling gated test already has an agent-login helper (e.g. `internal/cli/agent/integration_test.go`), copy its shape.

- [ ] **Step 3: Offline suite** — `cd /Users/jacinta/Source/cw && go build ./... && go vet ./... && go test ./...`
Expected: green; `TestLiveWhoamiRemote` SKIPs without `CW_IT_*`.

- [ ] **Step 4: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: README whoami --remote + gated live remote-whoami test"
```

- [ ] **Step 5: Controller — live smoke + merge** — provision an agent via `cw` (as cwadmin, like the #5/#6 smokes), export its `CW_IT_OWNER_SEED`/`CW_IT_AGENT_ID`/`CW_IT_AGENT_SLUG`, run `TestLiveWhoamiRemote` against dMon and/or `cw whoami --remote` directly (the endpoint is live). Then PR + merge.

---

## Self-review

**Spec coverage (#7b):**
- `internal/herald.UserInfo` + `Me(ctx,c)` (GET /api/me → bare UserInfo) → Task 1. ✔
- `cw whoami --remote` flag + authoritative render (org id+name, status, server scopes, agent responsible_human/fingerprint conditional) + `--json` → Task 2. ✔
- default whoami unchanged (only the writer switches to cmd.OutOrStdout, runtime-identical) → Task 2. ✔
- bare-UserInfo decode + 401 error mapping → Task 1; `--remote` wiring + json → Task 2. ✔
- README + gated live agent-remote test → Task 3. ✔

**Placeholder scan:** the `liveAgentClient` helper is the one implementer-judgment spot (mirror login.go's agent path / a sibling gated test); the controller's live smoke is the real verification.

**Type consistency:** `herald.{UserInfo, Me}`; `remoteWhoami(cmd, gf)` uses `session`→`herald.Me`→`cmd.OutOrStdout()`; `NewWhoamiCmd` adds the `--remote` BoolVar. Mirrors the existing wrapper + auth-package patterns. `os` import dropped from whoami.go if it becomes unused.
