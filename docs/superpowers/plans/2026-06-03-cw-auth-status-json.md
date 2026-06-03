# `--json` for `cw auth status` Implementation Plan (#8)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `cw auth status` honors the global `--json` flag (structured context entries) and lists contexts deterministically (sorted by name).

**Architecture:** One file, `internal/cli/auth/status.go`: build a sorted `[]statusEntry`, branch on `gf.JSON`, render via `cmd.OutOrStdout()`.

**Tech Stack:** Go 1.26, cobra, `sort`, `encoding/json`.

Sub-project **#8**. Spec: `docs/superpowers/specs/2026-06-03-cw-auth-status-json-design.md`.

## Verified facts

- `internal/cli/auth/status.go`: current `newStatusCmd` loops `cfg.Contexts` (map order), prints `%s%-12s %-28s %s (%s)\n` (marker/name/edge/display/state); state ∈ {valid,refreshable,logged-out} via `tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)` (`.Access()` live → valid; `.Refresh()` ok → refreshable; else logged-out); empty → "no contexts" hint.
- `config.Context{Edge, Identity config.Identity}`; `config.Identity{Kind,Subject,Display,Slug,Org}`; `cfg.CurrentContext`. `GlobalFlags.JSON` is the shared flag.
- Test helpers in package `auth`: `b64(s)` (`login_test.go`), the `keyring.MockInit()` + `config.Config{...}.Save()` + `tokenstore.New(edge,name,subject).SaveAccess(at, exp)` pattern (`whoami_test.go`).

---

## Task 1: `--json` + sorted listing for `cw auth status`

**Files:**
- Modify: `internal/cli/auth/status.go`
- Create: `internal/cli/auth/status_test.go`
- Modify: `README.md`

- [ ] **Step 1: Write the failing tests** — `internal/cli/auth/status_test.go`:

```go
package auth

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/zalando/go-keyring"
)

func TestStatusJSON(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{CurrentContext: "dev", Contexts: map[string]config.Context{
		"dev":  {Edge: "http://edge:8080", Identity: config.Identity{Kind: "human", Subject: "u1", Display: "alice@x"}},
		"prod": {Edge: "http://prod:8080", Identity: config.Identity{Kind: "agent", Subject: "a1", Display: "builder", Slug: "builder"}},
	}}
	_ = cfg.Save()
	at := "x." + b64(`{"sub":"u1","exp":9999999999}`) + ".y"
	_ = tokenstore.New("http://edge:8080", "dev", "u1").SaveAccess(at, time.Now().Add(time.Hour))

	cmd := newStatusCmd(&GlobalFlags{JSON: true})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var got []statusEntry
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(got) != 2 || got[0].Name != "dev" || got[1].Name != "prod" {
		t.Fatalf("want sorted [dev,prod], got %+v", got)
	}
	if !got[0].Current || got[1].Current {
		t.Fatalf("current flag wrong: %+v", got)
	}
	if got[0].State != "valid" {
		t.Fatalf("dev state = %q, want valid", got[0].State)
	}
	if got[0].Kind != "human" || got[1].Kind != "agent" || got[1].Display != "builder" {
		t.Fatalf("fields: %+v", got)
	}
}

func TestStatusJSONEmpty(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cmd := newStatusCmd(&GlobalFlags{JSON: true})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status --json (empty): %v", err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("empty status --json = %q, want []", out.String())
	}
}

func TestStatusTextSorted(t *testing.T) {
	keyring.MockInit()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{CurrentContext: "prod", Contexts: map[string]config.Context{
		"dev":  {Edge: "http://edge:8080", Identity: config.Identity{Kind: "human", Subject: "u1", Display: "alice@x"}},
		"prod": {Edge: "http://prod:8080", Identity: config.Identity{Kind: "agent", Subject: "a1", Display: "builder"}},
	}}
	_ = cfg.Save()
	cmd := newStatusCmd(&GlobalFlags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "dev") || !strings.Contains(lines[1], "prod") {
		t.Fatalf("want sorted dev then prod:\n%s", out.String())
	}
	if !strings.HasPrefix(lines[1], "* ") { // prod is current
		t.Fatalf("prod should have * marker:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run — expect FAIL** — `cd /Users/jacinta/Source/cw && go test ./internal/cli/auth/ -run TestStatus`
Expected: build error (`statusEntry` undefined).

- [ ] **Step 3: Rewrite `internal/cli/auth/status.go`**:

```go
package auth

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

// statusEntry is one context's freshness for `cw auth status`.
type statusEntry struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Edge    string `json:"edge"`
	Kind    string `json:"kind,omitempty"`
	Display string `json:"display,omitempty"`
	Subject string `json:"subject,omitempty"`
	Org     string `json:"org,omitempty"`
	State   string `json:"state"`
}

func newStatusCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List contexts and token freshness",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(cfg.Contexts))
			for name := range cfg.Contexts {
				names = append(names, name)
			}
			sort.Strings(names)

			entries := make([]statusEntry, 0, len(names))
			for _, name := range names {
				ctx := cfg.Contexts[name]
				// valid (cached access still live) > refreshable (refresh token
				// present) > logged-out.
				state := "logged-out"
				st := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
				if _, exp, err := st.Access(); err == nil && time.Until(exp) > 0 {
					state = "valid"
				} else if _, rerr := st.Refresh(); rerr == nil {
					state = "refreshable"
				}
				entries = append(entries, statusEntry{
					Name:    name,
					Current: name == cfg.CurrentContext,
					Edge:    ctx.Edge,
					Kind:    ctx.Identity.Kind,
					Display: ctx.Identity.Display,
					Subject: ctx.Identity.Subject,
					Org:     ctx.Identity.Org,
					State:   state,
				})
			}

			out := cmd.OutOrStdout()
			if gf.JSON {
				return json.NewEncoder(out).Encode(entries)
			}
			if len(entries) == 0 {
				fmt.Fprintln(out, "no contexts (run 'cw auth login --edge <url>')")
				return nil
			}
			for _, e := range entries {
				marker := "  "
				if e.Current {
					marker = "* "
				}
				fmt.Fprintf(out, "%s%-12s %-28s %s (%s)\n", marker, e.Name, e.Edge, e.Display, e.State)
			}
			return nil
		},
	}
}
```

- [ ] **Step 4: Run — expect PASS** — `cd /Users/jacinta/Source/cw && go test ./internal/cli/auth/ -v && go build ./... && go test ./... && go vet ./...`
Expected: `TestStatusJSON`/`TestStatusJSONEmpty`/`TestStatusTextSorted` + the existing auth tests PASS; full suite green. `go run ./cmd/cw auth status --json` emits a JSON array (or `[]`).

- [ ] **Step 5: README** — update the `cw auth status` mention under `## Identity` (around the line `cw auth status       # all contexts + token freshness`) to add the `--json` variant:
```markdown
    cw auth status          # all contexts + token freshness
    cw auth status --json   # the same, as a JSON array (name/current/edge/kind/display/subject/org/state)
```

- [ ] **Step 6: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/cli/auth/status.go internal/cli/auth/status_test.go README.md
git commit -m "cw auth status: --json output + deterministic (sorted) listing"
```

- [ ] **Step 7: Controller — smoke + merge** — `cw auth status --json` after a login (or the unit coverage is enough); PR + merge.

---

## Self-review

**Spec coverage (#8):**
- `statusEntry` (name/current/edge/kind/display/subject/org/state) → Task 1. ✔
- sorted `[]statusEntry`; `--json` → JSON array (`[]` when empty, non-nil slice via `make`); default → existing text table (now sorted) → Task 1. ✔
- `cmd.OutOrStdout()` render; state semantics + marker + format unchanged → Task 1. ✔
- README `--json` note → Task 1 Step 5. ✔
- tests: json (sorted, current flag, state, fields), empty `[]`, text sorted + marker → Task 1 Step 1. ✔

**Placeholder scan:** none.

**Type consistency:** `statusEntry` json tags; `newStatusCmd(gf)` uses `cmd.OutOrStdout()` + `gf.JSON`; reuses `config`/`tokenstore`. The closure signature changes `func(_ *cobra.Command,...)` → `func(cmd *cobra.Command,...)` to access `cmd.OutOrStdout()`. Consistent with the whoami pattern.
