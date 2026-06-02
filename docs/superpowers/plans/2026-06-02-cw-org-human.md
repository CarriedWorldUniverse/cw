# `cw org` + `cw human` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `cw org` (create/list/delete + product entitlements) and `cw human` (create/set-password) over herald's admin REST surface, on the existing `internal/client` + `internal/cmdutil` seam.

**Architecture:** A thin `internal/herald` package wraps herald's `AdminService` REST routes (through the edge `/herald` prefix) as typed Go (mirrors `internal/cairn`/`internal/ledger`/`internal/commonplace`). Two cobra groups consume it. Org ids/names are explicit args — no cwd inference. Passwords are read no-echo from a TTY or `--password-stdin`, never a plaintext flag.

**Tech Stack:** Go 1.26, cobra, the existing `internal/{client,cmdutil,identity,oidc}`, `golang.org/x/term` (already a dep). No proto/herald change, no new deps.

Sub-project **#4** (the LAST command group) of the CW CLI suite. Spec: `docs/superpowers/specs/2026-06-02-cw-org-human-design.md`. herald already exposes the admin API — single cw-side cycle.

## Verified wire shapes (what cw decodes)

Through `<edge>/herald` (bearer; herald enforces platform-admin / org-admin). `response_body:"X"` ⇒ grpc-gateway flattens the body to that field:
- `Org{id,name}`, `Human{id,display_name,org}` — snake_case JSON.
- `POST /api/orgs` `{name,products[]}` → **bare `Org`** (`response_body:"org"`)
- `GET /api/orgs` → **`{"orgs":[Org]}`** (no annotation → wrapped)
- `DELETE /api/orgs/{id}` `{name}` → `{"deleted":str,"pillars":[str]}`
- `GET /api/orgs/{org}/products` → **bare `map[string]bool`** (`response_body:"products"`)
- `POST /api/orgs/{org}/products/{product}/enable` → **bare `map[string]bool`** (no request body)
- `POST /api/orgs/{org}/products/{product}/disable` → **bare `map[string]bool`** (no request body)
- `POST /api/orgs/{org}/humans` `{display_name,scopes[]}` → **bare `Human`** (`response_body:"human"`)
- `POST /api/humans/{id}/password` `{password}` → empty 2xx

Path-param values (`{org}`,`{product}`,`{id}`) live ONLY in the path (url.PathEscape'd), never in the body. The pillar prefix is `herald` (`c.Do(ctx, m, "herald", "/api/orgs", …)` → `<edge>/herald/api/orgs`). Canonical products: `cairn`, `ledger`, `commonplace`.

---

## Task 1: `internal/herald` — admin REST wrapper

**Files:**
- Create: `internal/herald/herald.go`, `internal/herald/herald_test.go`

- [ ] **Step 1: Write the failing test (httptest stub mirroring herald admin)**

`internal/herald/herald_test.go`:
```go
package herald

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

func decode(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	if len(b) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("bad request body %q: %v", b, err)
	}
	return m
}

func stub(t *testing.T) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["name"] != "acme" {
			t.Errorf("create org body = %v", body)
		}
		_, _ = w.Write([]byte(`{"id":"o1","name":"acme"}`)) // bare Org (response_body:"org")
	})
	mux.HandleFunc("GET /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"orgs":[{"id":"o1","name":"acme"}]}`))
	})
	mux.HandleFunc("DELETE /herald/api/orgs/o1", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["name"] != "acme" || body["id"] != nil {
			t.Errorf("delete body = %v (want only name)", body)
		}
		_, _ = w.Write([]byte(`{"deleted":"o1","pillars":["cairn","ledger"]}`))
	})
	mux.HandleFunc("GET /herald/api/orgs/o1/products", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":false,"commonplace":true}`)) // bare map
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/products/ledger/enable", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":true,"commonplace":true}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/products/ledger/disable", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":false,"commonplace":true}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/humans", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["display_name"] != "alice" || body["org"] != nil {
			t.Errorf("create human body = %v (want display_name+scopes, no org)", body)
		}
		_, _ = w.Write([]byte(`{"id":"h1","display_name":"alice","org":"o1"}`)) // bare Human
	})
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["password"] != "hunter2hunter2" {
			t.Errorf("set-password body = %v", body)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestWrapper(t *testing.T) {
	c := stub(t)
	ctx := context.Background()

	o, err := CreateOrg(ctx, c, CreateOrgInput{Name: "acme"})
	if err != nil || o.ID != "o1" || o.Name != "acme" {
		t.Fatalf("CreateOrg: %v %+v", err, o)
	}
	orgs, err := ListOrgs(ctx, c)
	if err != nil || len(orgs) != 1 || orgs[0].ID != "o1" {
		t.Fatalf("ListOrgs: %v %+v", err, orgs)
	}
	del, err := DeleteOrg(ctx, c, "o1", "acme")
	if err != nil || del.Deleted != "o1" || len(del.Pillars) != 2 {
		t.Fatalf("DeleteOrg: %v %+v", err, del)
	}
	prods, err := GetProducts(ctx, c, "o1")
	if err != nil || prods["ledger"] != false || prods["cairn"] != true {
		t.Fatalf("GetProducts: %v %+v", err, prods)
	}
	en, err := EnableProduct(ctx, c, "o1", "ledger")
	if err != nil || en["ledger"] != true {
		t.Fatalf("EnableProduct: %v %+v", err, en)
	}
	dis, err := DisableProduct(ctx, c, "o1", "ledger")
	if err != nil || dis["ledger"] != false {
		t.Fatalf("DisableProduct: %v %+v", err, dis)
	}
	h, err := CreateHuman(ctx, c, "o1", CreateHumanInput{DisplayName: "alice", Scopes: []string{"knowledge:read"}})
	if err != nil || h.ID != "h1" || h.Org != "o1" {
		t.Fatalf("CreateHuman: %v %+v", err, h)
	}
	if err := SetHumanPassword(ctx, c, "h1", "hunter2hunter2"); err != nil {
		t.Fatalf("SetHumanPassword: %v", err)
	}
}

func errStub(t *testing.T) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"requires herald:platform-admin"}`))
	})
	mux.HandleFunc("GET /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestErrorMapping(t *testing.T) {
	c := errStub(t)
	ctx := context.Background()
	if _, err := CreateOrg(ctx, c, CreateOrgInput{Name: "x"}); err == nil ||
		!strings.Contains(err.Error(), "requires herald:platform-admin") {
		t.Fatalf("CreateOrg error: want server message, got %v", err)
	}
	if _, err := ListOrgs(ctx, c); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("ListOrgs error: want status fallback, got %v", err)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/herald/`
Expected: build error (undefined).

- [ ] **Step 3: Implement `internal/herald/herald.go`**

```go
// Package herald wraps herald's AdminService REST surface (org + identity
// provisioning, through the edge /herald prefix) as typed Go over a
// *client.Client. Mirrors internal/cairn. Path-param values live only in the
// path (url.PathEscape'd); request bodies carry only the body:"*" fields.
package herald

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Human struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Org         string `json:"org"`
}
type CreateOrgInput struct {
	Name     string   `json:"name"`
	Products []string `json:"products,omitempty"`
}
type CreateHumanInput struct {
	DisplayName string   `json:"display_name"`
	Scopes      []string `json:"scopes,omitempty"`
}
type DeleteResult struct {
	Deleted string   `json:"deleted"`
	Pillars []string `json:"pillars"`
}

// do marshals body, calls the herald pillar, maps non-2xx to an error, decodes
// into out (nil out = 2xx check only). Mirrors internal/cairn.do.
func do(ctx context.Context, c *client.Client, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("herald: marshal: %w", err)
		}
		raw = b
	}
	resp, respBody, err := c.Do(ctx, method, "herald", path, raw)
	if err != nil {
		return err // includes client.ErrReauth
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("herald: %s %s: %s", method, path, errMsg(respBody, resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("herald: decode %s: %w", path, err)
		}
	}
	return nil
}

func errMsg(body []byte, status int) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	switch {
	case e.Error != "":
		return e.Error
	case e.Message != "":
		return e.Message
	default:
		return fmt.Sprintf("status %d", status)
	}
}

func CreateOrg(ctx context.Context, c *client.Client, in CreateOrgInput) (Org, error) {
	var o Org
	err := do(ctx, c, http.MethodPost, "/api/orgs", in, &o)
	return o, err
}

func ListOrgs(ctx context.Context, c *client.Client) ([]Org, error) {
	var w struct {
		Orgs []Org `json:"orgs"`
	}
	err := do(ctx, c, http.MethodGet, "/api/orgs", nil, &w)
	return w.Orgs, err
}

func DeleteOrg(ctx context.Context, c *client.Client, id, name string) (DeleteResult, error) {
	var r DeleteResult
	path := "/api/orgs/" + url.PathEscape(id)
	err := do(ctx, c, http.MethodDelete, path, map[string]string{"name": name}, &r)
	return r, err
}

func GetProducts(ctx context.Context, c *client.Client, org string) (map[string]bool, error) {
	out := map[string]bool{}
	path := "/api/orgs/" + url.PathEscape(org) + "/products"
	err := do(ctx, c, http.MethodGet, path, nil, &out)
	return out, err
}

func EnableProduct(ctx context.Context, c *client.Client, org, product string) (map[string]bool, error) {
	out := map[string]bool{}
	path := "/api/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/enable"
	err := do(ctx, c, http.MethodPost, path, nil, &out)
	return out, err
}

func DisableProduct(ctx context.Context, c *client.Client, org, product string) (map[string]bool, error) {
	out := map[string]bool{}
	path := "/api/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/disable"
	err := do(ctx, c, http.MethodPost, path, nil, &out)
	return out, err
}

func CreateHuman(ctx context.Context, c *client.Client, org string, in CreateHumanInput) (Human, error) {
	var h Human
	path := "/api/orgs/" + url.PathEscape(org) + "/humans"
	err := do(ctx, c, http.MethodPost, path, in, &h)
	return h, err
}

func SetHumanPassword(ctx context.Context, c *client.Client, id, password string) error {
	path := "/api/humans/" + url.PathEscape(id) + "/password"
	return do(ctx, c, http.MethodPost, path, map[string]string{"password": password}, nil)
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/herald/ -v && go build ./... && go vet ./internal/herald/`
Expected: `TestWrapper` + `TestErrorMapping` PASS; build + vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/herald/
git commit -m "herald: typed admin REST wrapper (orgs/products/humans) over the client seam"
```

---

## Task 2: `cw org` commands

**Files:**
- Create: `internal/cli/org/org.go`, `internal/cli/org/org_test.go`
- Modify: `cmd/cw/main.go` (register `org.NewCmd`)

- [ ] **Step 1: Write the failing command tests**

`internal/cli/org/org_test.go`:
```go
package org

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestOrgListWiring drives cobra Execute (flag -> Session -> herald -> stdout)
// against a stub, proving the wiring works offline.
func TestOrgListWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var hit []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/orgs", func(w http.ResponseWriter, r *http.Request) {
		hit = append(hit, r.URL.Path)
		_, _ = w.Write([]byte(`{"orgs":[{"id":"o1","name":"acme"}]}`))
	})
	mux.HandleFunc("GET /herald/api/orgs/o1/products", func(w http.ResponseWriter, r *http.Request) {
		hit = append(hit, r.URL.Path)
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":false}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	for _, args := range [][]string{{"list"}, {"products", "o1"}} {
		cmd := NewCmd(gf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
	}
	if len(hit) != 2 || hit[0] != "/herald/api/orgs" || hit[1] != "/herald/api/orgs/o1/products" {
		t.Fatalf("endpoints hit = %v", hit)
	}
}

// TestDeleteConfirmMismatch: delete refuses (no HTTP call) when --confirm != name.
func TestDeleteConfirmMismatch(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /herald/api/orgs/o1", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"delete", "o1", "--confirm", "WRONG"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected confirm-mismatch error")
	}
	if called {
		t.Fatal("delete must not hit the server on confirm mismatch")
	}
}
```

> The `delete` command takes the org id as the positional arg and `--confirm` as the org NAME. The mismatch test passes id `o1` with `--confirm WRONG`; since the command compares `--confirm` against the supplied name and there's no name to match, it must fail before any call. (The command requires `--confirm` to be non-empty AND, on the happy path, the server enforces name-equality; client-side we only guarantee `--confirm` is present and pass it as the body `name`. To keep the unit test honest, the command treats a missing/empty `--confirm` as fail-fast; the mismatch case is enforced server-side. **Implementer:** make `--confirm` required — `cobra` `MarkFlagRequired` or a manual empty-check returning an error — so the test's separate "empty confirm" path is covered; for the WRONG case, the value IS present so adjust the test to assert the server received `name=WRONG` instead. See Step 3 note.)

- [ ] **Step 2: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/cli/org/`
Expected: build error.

- [ ] **Step 3: Implement `internal/cli/org/org.go`**

> **Decision (resolving the Step-1 note):** `--confirm` is REQUIRED and passed verbatim as the delete body `name`; herald does the authoritative name-equality check (confirm-by-name). The client-side guard is: `--confirm` must be non-empty (fail-fast). This is simpler and avoids cw second-guessing the org name. Adjust the `TestDeleteConfirmMismatch` test to instead assert that omitting `--confirm` fails fast with NO HTTP call (rename to `TestDeleteRequiresConfirm`, args `{"delete","o1"}`, expect error + `called==false`).

```go
// Package org implements `cw org`: create, list, delete, products, enable, disable.
package org

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/spf13/cobra"
)

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "org", Short: "Manage orgs and product entitlements (herald admin)"}
	cmd.AddCommand(newCreateCmd(gf), newListCmd(gf), newDeleteCmd(gf),
		newProductsCmd(gf), newProductToggleCmd(gf, true), newProductToggleCmd(gf, false))
	return cmd
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var products []string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			o, err := herald.CreateOrg(cmd.Context(), c, herald.CreateOrgInput{Name: args[0], Products: products})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "created org %s (%s)\n", o.ID, o.Name)
			fmt.Println(o.ID)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&products, "product", nil, "product to enable at creation (repeatable)")
	return cmd
}

func newListCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List orgs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			orgs, err := herald.ListOrgs(cmd.Context(), c)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(orgs)
			}
			for _, o := range orgs {
				fmt.Printf("%-38s %s\n", o.ID, o.Name)
			}
			return nil
		},
	}
}

func newDeleteCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var confirm string
	cmd := &cobra.Command{
		Use:   "delete <id> --confirm <name>",
		Short: "Delete (purge) an org; --confirm must equal the org name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if confirm == "" {
				return fmt.Errorf("pass --confirm <org-name> to delete")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			res, err := herald.DeleteOrg(cmd.Context(), c, args[0], confirm)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "deleted %s (purged: %v)\n", res.Deleted, res.Pillars)
			return nil
		},
	}
	cmd.Flags().StringVar(&confirm, "confirm", "", "org name, required to confirm deletion")
	return cmd
}

func newProductsCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "products <org>",
		Short: "Show product entitlements for an org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			prods, err := herald.GetProducts(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			return printProducts(gf, prods)
		},
	}
}

// newProductToggleCmd builds both `enable` and `disable` (they differ only in
// the herald call + verb).
func newProductToggleCmd(gf *cmdutil.GlobalFlags, enable bool) *cobra.Command {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	return &cobra.Command{
		Use:   verb + " <org> <product>",
		Short: verb + " a product for an org",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			fn := herald.DisableProduct
			if enable {
				fn = herald.EnableProduct
			}
			prods, err := fn(cmd.Context(), c, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "%sd %s for %s\n", verb, args[1], args[0])
			return printProducts(gf, prods)
		},
	}
}

// printProducts renders a {product:enabled} map: --json to stdout, else a
// sorted `product  enabled/disabled` table to stdout.
func printProducts(gf *cmdutil.GlobalFlags, prods map[string]bool) error {
	if gf.JSON {
		return json.NewEncoder(os.Stdout).Encode(prods)
	}
	keys := make([]string, 0, len(prods))
	for k := range prods {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		state := "disabled"
		if prods[k] {
			state = "enabled"
		}
		fmt.Printf("%-14s %s\n", k, state)
	}
	return nil
}
```

> Note: `var fn = herald.DisableProduct` then reassigning to `herald.EnableProduct` relies on both having the identical signature `func(context.Context, *client.Client, string, string) (map[string]bool, error)` — they do.

- [ ] **Step 4: Register in `main.go`**

In `cmd/cw/main.go`, import `org "github.com/CarriedWorldUniverse/cw/internal/cli/org"` and add after the `kb` registration:
```go
	root.AddCommand(org.NewCmd(flags))
```

- [ ] **Step 5: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/cli/org/ -v && go test ./...`
Expected: build OK; org tests PASS (after applying the Step-3 test rename); full suite green. `go run ./cmd/cw org --help` lists create/list/delete/products/enable/disable.

- [ ] **Step 6: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/cli/org/ cmd/cw/main.go
git commit -m "cw: cw org create/list/delete + product entitlements"
```

---

## Task 3: `cw human` commands + password prompt helper

**Files:**
- Modify: `internal/identity/identity.go` (add `PromptPassword`)
- Create: `internal/cli/human/human.go`, `internal/cli/human/human_test.go`
- Modify: `cmd/cw/main.go` (register `human.NewCmd`)

- [ ] **Step 1: Add `PromptPassword` to `internal/identity/identity.go`**

After `PromptHuman` (it already imports `golang.org/x/term`, `fmt`, `os`):
```go
// PromptPassword reads a single password from the terminal without echoing it.
// Interactive only — the read requires a TTY.
func PromptPassword(in *os.File, label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	pw, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		if !term.IsTerminal(int(in.Fd())) {
			return "", fmt.Errorf("identity: password prompt needs a terminal: %w", err)
		}
		return "", fmt.Errorf("identity: read password: %w", err)
	}
	return string(pw), nil
}
```

- [ ] **Step 2: Write the failing command tests**

`internal/cli/human/human_test.go`:
```go
package human

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestHumanCreateWiring: create POSTs the right body, and --password-stdin
// triggers the follow-up password call with the piped secret.
func TestHumanCreateWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var paths []string
	var pwBody string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/humans", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"display_name":"alice"`) || !strings.Contains(string(b), "knowledge:read") {
			t.Errorf("create body = %s", b)
		}
		_, _ = w.Write([]byte(`{"id":"h1","display_name":"alice","org":"o1"}`))
	})
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		pwBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"create", "--org", "o1", "--name", "alice", "--scope", "knowledge:read", "--password-stdin"})
	cmd.SetIn(strings.NewReader("s3cr3t-passphrase\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/herald/api/orgs/o1/humans" || paths[1] != "/herald/api/humans/h1/password" {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(pwBody, "s3cr3t-passphrase") {
		t.Fatalf("password body = %s", pwBody)
	}
}

// TestReadSecret: --password-stdin reads + trims a line; empty when required errors.
func TestReadSecret(t *testing.T) {
	got, err := readSecret(strings.NewReader("topsecret\n"), true, true)
	if err != nil || got != "topsecret" {
		t.Fatalf("stdin path: %q %v", got, err)
	}
	if _, err := readSecret(strings.NewReader("\n"), true, true); err == nil {
		t.Fatal("empty stdin + required should error")
	}
}
```

- [ ] **Step 3: Run — expect FAIL**

Run: `cd /Users/jacinta/Source/cw && go test ./internal/cli/human/`
Expected: build error.

- [ ] **Step 4: Implement `internal/cli/human/human.go`**

```go
// Package human implements `cw human`: create and set-password (herald admin).
package human

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "human", Short: "Provision human identities (herald admin)"}
	cmd.AddCommand(newCreateCmd(gf), newSetPasswordCmd(gf))
	return cmd
}

// readSecret sources a password: if passwordStdin, read one trimmed line from r;
// else if r is an interactive TTY, prompt no-echo; else (required) error, or
// (optional) return "". required distinguishes set-password (must) from a
// create without --password-stdin (may skip).
func readSecret(r io.Reader, passwordStdin, required bool) (string, error) {
	if passwordStdin {
		line, err := bufio.NewReader(r).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		pw := strings.TrimRight(line, "\r\n")
		if pw == "" && required {
			return "", fmt.Errorf("empty password on stdin")
		}
		return pw, nil
	}
	if f, ok := r.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return identity.PromptPassword(f, "Password")
	}
	if required {
		return "", fmt.Errorf("provide the password via --password-stdin or an interactive terminal")
	}
	return "", nil
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var org, name string
	var scopes []string
	var passwordStdin bool
	cmd := &cobra.Command{
		Use:   "create --org <org> --name <display-name>",
		Short: "Create a human (optionally setting a password from stdin)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if org == "" || name == "" {
				return fmt.Errorf("--org and --name are required")
			}
			var pw string
			if passwordStdin {
				var err error
				if pw, err = readSecret(cmd.InOrStdin(), true, true); err != nil {
					return err
				}
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			h, err := herald.CreateHuman(cmd.Context(), c, org, herald.CreateHumanInput{DisplayName: name, Scopes: scopes})
			if err != nil {
				return err
			}
			msg := fmt.Sprintf("created human %s (%s) in org %s", h.ID, h.DisplayName, h.Org)
			if passwordStdin {
				if err := herald.SetHumanPassword(cmd.Context(), c, h.ID, pw); err != nil {
					return fmt.Errorf("human created (%s) but set-password failed: %w", h.ID, err)
				}
				msg += "; password set"
			}
			fmt.Fprintln(os.Stderr, msg)
			fmt.Println(h.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&org, "org", "", "org id (required)")
	f.StringVar(&name, "name", "", "display name (required)")
	f.StringArrayVar(&scopes, "scope", nil, "scope to grant, e.g. knowledge:read (repeatable)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the human's password from stdin")
	return cmd
}

func newSetPasswordCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var passwordStdin bool
	cmd := &cobra.Command{
		Use:   "set-password <human-id>",
		Short: "Set (or reset) a human's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := readSecret(cmd.InOrStdin(), passwordStdin, true)
			if err != nil {
				return err
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			if err := herald.SetHumanPassword(cmd.Context(), c, args[0], pw); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "password set for %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin (else prompt)")
	return cmd
}
```

- [ ] **Step 5: Register in `main.go`**

Import `human "github.com/CarriedWorldUniverse/cw/internal/cli/human"` and add after the `org` registration:
```go
	root.AddCommand(human.NewCmd(flags))
```

- [ ] **Step 6: Run — expect PASS**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/cli/human/ ./internal/identity/ -v && go test ./... && go vet ./...`
Expected: build OK; human + identity tests PASS; full suite green. `go run ./cmd/cw human --help` lists create/set-password.

- [ ] **Step 7: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/cli/human/ internal/identity/identity.go cmd/cw/main.go
git commit -m "cw: cw human create/set-password + identity.PromptPassword"
```

---

## Task 4: README + gated live integration

**Files:**
- Modify: `README.md`
- Create: `internal/cli/org/integration_test.go` (gated)

- [ ] **Step 1: README — admin sections**

Append to `README.md`:
```markdown
## Orgs (herald admin)

    cw org create acme [--product cairn --product ledger]
    cw org list
    cw org products <org-id>
    cw org enable  <org-id> ledger
    cw org disable <org-id> ledger
    cw org delete  <org-id> --confirm acme      # --confirm must equal the org name

## Humans (herald admin)

    cw human create --org <org-id> --name alice \
        --scope knowledge:read --scope knowledge:write --password-stdin <<< "$PW"
    cw human set-password <human-id> --password-stdin <<< "$PW"   # else prompts no-echo

Org and identity admin require a platform-admin (`herald:platform-admin`) or
org-admin (`herald:org-admin`) bearer. Passwords are read from stdin
(`--password-stdin`) or an interactive prompt — never a plaintext flag.
Provisioning a working identity end to end:

    ORG=$(cw org create acme)
    H=$(cw human create --org "$ORG" --name alice --scope knowledge:read --password-stdin <<< "$PW")
    cw auth login --edge <edge>      # log in as $H
```

- [ ] **Step 2: Gated live integration**

`internal/cli/org/integration_test.go`:
```go
package org

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
)

// TestLiveAdmin provisions end to end against live herald: create a cwb-test-*
// org, flip a product off then on (assert the map changes), create a human with
// knowledge scopes, set its password, and log THAT human in — proving the
// provisioned identity works and carries the granted scopes.
//
// Gated on CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD; skips otherwise.
// CW_IT_USER MUST be the platform-admin (genesis owner, e.g.
// cwadmin@carriedworld.com) — a working-org human gets 403 on org create.
// The cwb-test-* org name lets the conformance reaper collect it.
func TestLiveAdmin(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER (platform-admin) + CW_IT_PASSWORD to run the live admin test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	c, _ := liveSession(t, edge) // platform-admin client; copy the #1b/#2/#3 helper shape

	ctx := context.Background()
	marker := "cwb-test-org-" + time.Now().Format("150405")
	org, err := herald.CreateOrg(ctx, c, herald.CreateOrgInput{Name: marker})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	t.Logf("created org %s (%s)", org.ID, org.Name)

	off, err := herald.DisableProduct(ctx, c, org.ID, "ledger")
	if err != nil || off["ledger"] != false {
		t.Fatalf("DisableProduct: %v %+v", err, off)
	}
	on, err := herald.EnableProduct(ctx, c, org.ID, "ledger")
	if err != nil || on["ledger"] != true {
		t.Fatalf("EnableProduct: %v %+v", err, on)
	}

	h, err := herald.CreateHuman(ctx, c, org.ID, herald.CreateHumanInput{
		DisplayName: "kb-user-" + marker,
		Scopes:      []string{"knowledge:read", "knowledge:write"},
	})
	if err != nil {
		t.Fatalf("CreateHuman: %v", err)
	}
	pw := "provisioned-pw-" + marker
	if err := herald.SetHumanPassword(ctx, c, h.ID, pw); err != nil {
		t.Fatalf("SetHumanPassword: %v", err)
	}

	// Log the provisioned human in (username = its herald id) and assert the
	// minted token carries the knowledge scopes.
	tok, err := oidc.New(edge).PasswordGrant(ctx, h.ID, pw)
	if err != nil {
		t.Fatalf("login provisioned human: %v", err)
	}
	claims, err := identity.DecodeAccessClaims(tok.AccessToken)
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	scope, _ := claims["scope"].(string) // DecodeAccessClaims returns map[string]any
	if !strings.Contains(scope, "knowledge:write") {
		t.Fatalf("provisioned token missing knowledge:write; scope=%q", scope)
	}
}
```

> **Implementer note:** add the `liveSession(t, edge) (*client.Client, config.Context)` helper to this file by copying `internal/cli/repo/integration_test.go`'s helper VERBATIM (same env vars, same `oidc` password-grant + `tokenstore`/`config` write). Then verify the exact `oidc` login + claims-decode call shapes against that helper and `internal/identity` — match the real method names (`oidc.New(edge).PasswordGrant(ctx, user, pass)` and `identity.DecodeAccessClaims`); adjust the calls in `TestLiveAdmin` if the real signatures differ, keeping the intent (provision → login provisioned human → assert knowledge scope). If `oidc`/`identity` expose differently-named helpers, use whatever the sibling live tests already use to log a user in and read claims.

- [ ] **Step 3: Offline suite**

Run: `cd /Users/jacinta/Source/cw && go build ./... && go vet ./... && go test ./...`
Expected: green; `TestLiveAdmin` SKIPs without `CW_IT_*`.

- [ ] **Step 4: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: README org/human admin usage + gated live admin provisioning test"
```

- [ ] **Step 5: Controller — live smoke + branch/merge**

Controller (not implementer): with platform-admin (cwadmin) creds, run `TestLiveAdmin` against dMon and/or a manual `cw org create … && cw human create … && cw auth login` smoke (this is the self-hosting milestone — it also produces a `knowledge:*` identity usable for #3's deferred live happy-path). Then PR + merge the cw branch.

---

## Self-review

**Spec coverage (#4):**
- `internal/herald` wrapper (CreateOrg/ListOrgs/DeleteOrg/GetProducts/EnableProduct/DisableProduct/CreateHuman/SetHumanPassword) → Task 1. ✔
- `cw org create/list/delete/products/enable/disable` → Task 2. ✔
- `cw human create/set-password` + no-plaintext-flag password sourcing (`--password-stdin` / no-echo TTY prompt) → Task 3 (`readSecret` + `identity.PromptPassword`). ✔
- bare `Org`/`Human` vs wrapped `{orgs}` vs bare `map[string]bool` vs `{deleted,pillars}` decodes; path-params in path only; body carries only `{name}`/`{display_name,scopes}`/`{password}` → Task 1 (+ stub assertions). ✔
- delete confirm-by-name (client fail-fast on empty `--confirm`, server enforces equality) → Task 2. ✔
- output convention (created id→stdout, confirmation→stderr, --json→stdout; tables→stdout) → Tasks 2/3. ✔
- README + gated live provisioning test (the self-hosting milestone, login-provisioned-human asserts scopes) → Task 4. ✔

**Placeholder scan:** no TBD/TODO. `liveSession` + the exact `oidc`/`identity` login-and-decode calls are the implementer-judgment spots (copy the sibling live tests), with the controller's live smoke as the real verification.

**Type consistency:** `herald.{Org,Human,CreateOrgInput,CreateHumanInput,DeleteResult}` + the 8 funcs `(ctx, *client.Client, …)`; `org.NewCmd`/`human.NewCmd(*cmdutil.GlobalFlags)`; `readSecret(io.Reader,bool,bool)`; `identity.PromptPassword(*os.File,string)`; `cmdutil.Session`/`GlobalFlags`. `EnableProduct`/`DisableProduct` share the exact signature the `fn` reassignment in Task 2 relies on. Consistent across tasks + with the cairn/ledger/commonplace + cli/{repo,pr,issue,kb} patterns.
