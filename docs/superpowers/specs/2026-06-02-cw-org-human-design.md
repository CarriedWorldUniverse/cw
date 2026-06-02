# `cw org` + `cw human` — design

**Date:** 2026-06-02
**Status:** design (approved in brainstorming)
**Sub-project:** #4 of the CW CLI suite — the herald admin (org/identity provisioning) command groups. The LAST group. Single cw-side cycle (herald already exposes the admin REST surface through the gateway). Builds on the #0b `internal/client` seam + the `internal/cmdutil` + `internal/<pillar>`-wrapper idiom established by #1/#2/#3.

## Goal

Replace the manual `curl`-to-herald provisioning done by hand throughout the CW CLI effort with first-class commands. After this lands, `cw` can bootstrap working identities itself — orgs, product entitlements, and humans with scopes — including the kind of `knowledge:*` human that #3's live happy-path test needed.

## Platform grounding (what herald exposes)

Herald's `AdminService` is fronted by interchange at `<edge>/herald/api/...` (JWT-authed, identity-derived authz; platform-admin or org-admin). The bearer is attached by the client seam; herald enforces the role and `cw` surfaces the 403. The reachable endpoints (HTTP bindings present in `cwb.herald.v1`) and their **verified wire shapes** (from the proto `response_body` annotations):

| cw surface | route | auth | request | response |
|---|---|---|---|---|
| org create | `POST /api/orgs` | platform-admin¹ | `{name, products[]}` | **bare `Org`** (`response_body:"org"`) |
| org list | `GET /api/orgs` | platform-admin | — | **`{"orgs":[Org]}`** |
| org delete | `DELETE /api/orgs/{id}` | platform-admin | `{name}` (confirm) | `{"deleted":str,"pillars":[str]}` |
| org products | `GET /api/orgs/{org}/products` | org-admin / platform-admin | — | **bare `map[string]bool`** (`response_body:"products"`) |
| org enable | `POST /api/orgs/{org}/products/{product}/enable` | org-admin / platform-admin | — | **bare `map[string]bool`** |
| org disable | `POST /api/orgs/{org}/products/{product}/disable` | org-admin / platform-admin | — | **bare `map[string]bool`** |
| human create | `POST /api/orgs/{org}/humans` | org-admin / platform-admin | `{display_name, scopes[]}` | **bare `Human`** (`response_body:"human"`) |
| human set-password | `POST /api/humans/{id}/password` | org-admin / platform-admin | `{password}` | empty (2xx) |

¹ `CreateOrg`'s proto comment anticipates "any authenticated principal" (NEX-413 signup), but the live code enforces platform-admin today; `cw` just sends the bearer and surfaces whatever herald decides.

`response_body:"X"` means the grpc-gateway flattens the HTTP body to that field — so `CreateOrg` returns a bare `Org` (not `{"org":...}`), the three product RPCs return a bare `{product: enabled}` map, and `CreateHuman` returns a bare `Human`. `ListOrgs` (no annotation) stays wrapped `{"orgs":[...]}`; `DeleteOrg` returns its whole message `{deleted, pillars}`. snake_case JSON.

**Canonical products:** `cairn`, `ledger`, `commonplace` (the deny-list: an absent override = enabled). Disabling writes an explicit `enabled=false` override; the gateway then blocks any call scoped to that org+product. Reversible; data untouched.

**Not reachable / out of scope** (herald exposes no HTTP binding, so `cw` cannot offer them): list/get/delete humans, list/get/delete agents, update-scopes (server-side `GrantScope` is creation-time-only/internal), whoami. **Agent creation deferred** — it needs ed25519 casket keypair generation + agent-private-key storage, which is its own design. **`IssueHumanToken` skipped** — it's a deprecated MVP login stand-in; `cw auth login` is the real path.

## Scope

**In:** `internal/herald` wrapper + `cw org create/list/delete/products/enable/disable` + `cw human create/set-password`.

**Out (future):** `cw agent create` (keypair gen + key storage); any list/get/delete of humans/agents (no server endpoint); `cw org update`; whoami. No herald/proto change in #4.

## Tech

Go 1.26; reuses `internal/client` (`Do([]byte)`/`URL`/`ErrReauth`) + `internal/cmdutil` (`Session`) + the existing no-echo password prompt used by `cw auth login`. No new deps. No org-resolution helper (org ids/names are explicit args/flags here, not cwd-inferred).

---

## `internal/herald` — admin REST wrapper

Mirrors `internal/cairn`/`internal/ledger`/`internal/commonplace`: a `do(ctx, c, method, path, body, out any) error` helper (marshal → `c.Do(ctx, method, "herald", path, raw)` → non-2xx → `errMsg` → decode; `client.ErrReauth` passes through) + `errMsg` extracting `{"error"|"message"}`. Pillar = `"herald"`. `url.PathEscape` on caller-supplied path segments (org id, product, human id).

```go
package herald

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

func CreateOrg(ctx, c, CreateOrgInput) (Org, error)                 // POST /api/orgs            -> bare Org
func ListOrgs(ctx, c) ([]Org, error)                               // GET  /api/orgs            -> {"orgs":[...]}
func DeleteOrg(ctx, c, id, name string) (DeleteResult, error)      // DELETE /api/orgs/{id}     -> {deleted,pillars}
func GetProducts(ctx, c, org string) (map[string]bool, error)      // GET  .../products         -> bare map
func EnableProduct(ctx, c, org, product string) (map[string]bool, error)   // POST .../enable   -> bare map
func DisableProduct(ctx, c, org, product string) (map[string]bool, error)  // POST .../disable  -> bare map
func CreateHuman(ctx, c, org string, in CreateHumanInput) (Human, error)   // POST .../humans   -> bare Human
func SetHumanPassword(ctx, c, id, password string) error           // POST /api/humans/{id}/password -> 2xx-only
```

- `body:"*"` RPCs (`CreateOrg`, `DeleteOrg`, `CreateHuman`, `SetHumanPassword`) take a JSON body; the path-param values (`{org}`, `{id}`, `{product}`) are NOT in the body for the products/human-nested routes — they live only in the path. `DeleteOrg` sends `{name}` in the body (the `{id}` is the path). `CreateHuman` sends `{display_name, scopes}` (the `{org}` is the path). `SetHumanPassword` sends `{password}`.
- `ListOrgs` decodes a wrapper `struct{ Orgs []Org }`; the product RPCs decode straight into `map[string]bool`; the rest decode bare structs.

## `internal/cli/org` — `cw org` (on `cmdutil.Session`)

`NewCmd(*cmdutil.GlobalFlags)` → create/list/delete/products/enable/disable.

| Command | Behavior |
|---|---|
| `cw org create <name> [--product P ...]` | `--product` repeatable (`StringArrayVar`) for initial enablement. Prints new org id → stdout, confirmation → stderr. |
| `cw org list` | Table `id  name` → stdout, or `--json` (`[]Org`). |
| `cw org delete <id> --confirm <name>` | `--confirm` required and must equal the supplied name client-side (fail-fast "pass --confirm <org-name> to delete") before the call; herald re-checks. On success prints `deleted <id> (purged: pillar,pillar)` → stderr. |
| `cw org products <org>` | Table `product  enabled/disabled` (sorted keys) → stdout, or `--json` (`map[string]bool`). |
| `cw org enable <org> <product>` | Enables; prints resulting map confirmation → stderr (or `--json` map → stdout). |
| `cw org disable <org> <product>` | Disables; same output shape as enable. |

## `internal/cli/human` — `cw human` (on `cmdutil.Session`)

`NewCmd(*cmdutil.GlobalFlags)` → create/set-password.

| Command | Behavior |
|---|---|
| `cw human create --org <org> --name <dn> [--scope S ...] [--password-stdin]` | `--org` + `--name` required (fail-fast). `--scope` repeatable. Creates the human; if `--password-stdin`, reads one line from stdin and calls `SetHumanPassword` after create. Prints new human id → stdout, confirmation (+ "password set" when applicable) → stderr. |
| `cw human set-password <human-id> [--password-stdin]` | Password sourced via `readSecret`: `--password-stdin` reads one trimmed line from stdin; otherwise prompt no-echo on a TTY (reuse the `cw auth login` prompt). Empty → error. Calls `SetHumanPassword`. Confirmation → stderr. |

**Password sourcing (`readSecret`)** — never a plaintext `--password` flag (shell-history leak):
- `--password-stdin` set → read one line from stdin, trim trailing newline.
- else, if stdin is a TTY → no-echo prompt "Password: " (the helper `cw auth login` already uses for human login).
- else (piped, no `--password-stdin`) → for `set-password`, error "provide the password via --password-stdin or an interactive terminal"; for `create`, no password is set (human created password-less).
- A set password must be ≥8 chars (herald enforces; surfaced as a 400 if short).

## Data flows

- **provision a working identity (the headline loop):** `cw org create acme` → org id `O`; `cw human create --org O --name alice --scope knowledge:read --scope knowledge:write --password-stdin <<<"$PW"` → human id `H` (password set); `cw auth login --edge <edge>` as `H` → a usable token with `knowledge:*`. This is exactly what was done by hand for #1b/#2/#3.
- **gate a product:** `cw org disable O ledger` → map shows `ledger:false`; gateway now blocks ledger calls for org `O`; `cw org enable O ledger` reverses it.
- **list/inspect:** `cw org list` → table; `cw org products O` → entitlement map.

## Error handling

- non-2xx → herald's message: `requires herald:platform-admin` / `requires admin of org <id>` (403), confirm-by-name mismatch, password too short (400). `client.ErrReauth` → root's "session expired".
- `cw org delete` without `--confirm` or with a mismatching value → fail-fast client-side before any call.
- `cw human create` missing `--org`/`--name`, `cw human set-password` with an empty resolved password → fail-fast.

## Testing

- `internal/herald`: httptest-stub unit tests for all eight functions — bare `Org`/`Human` vs wrapped `{orgs}` vs bare `map[string]bool` vs `{deleted,pillars}` decodes; `SetHumanPassword` 2xx-only; a 403 scope-error mapping (`errMsg`); assert path-escaping + that path-param values are sent in the path (not the body) and the body carries only `{name}`/`{display_name,scopes}`/`{password}`.
- `internal/cli/org`: cobra-`Execute` wiring test (stub herald, `--token` static, run `org list` + `org products`, assert endpoints hit + output); a `delete` confirm-mismatch fail-fast test (no HTTP call made).
- `internal/cli/human`: a `create` wiring test (asserts the `POST /humans` body shape + that `--password-stdin` triggers the follow-up `POST /password`); a `readSecret` sourcing test (stdin path + empty→error), exercised with an injected reader.
- Gated live integration (`CW_IT_*` as **platform-admin** cwadmin, skips offline): create a `cwb-test-*` org (reaper collects it) → `disable` then `enable` a product (assert the returned map flips `false`→`true`) → `create` a human with `knowledge:read`/`knowledge:write` → `set-password` → log that human in via the `oidc` password grant and assert the token carries the knowledge scopes. The self-hosting milestone — and it produces the identity #3 wanted. NOTE: `CW_IT_USER` here must be the genesis owner (platform-admin); the existing `/tmp/cwsmoke.env` working-org human will get 403 on org create.

## Build order

Single cycle: `internal/herald` → `internal/cli/org` + `internal/cli/human` (register both in `cmd/cw/main.go` after `kb`) → README (`## Orgs (herald admin)` + `## Humans (herald admin)`) + gated live test. Reuses the client seam unchanged; inherits the shared hygiene (JSON Content-Type, query/path escaping, output convention).

## Future (deferred)

`cw agent create` (ed25519 keypair gen + casket pubkey registration + private-key storage — its own brainstorm), list/get/delete of humans+agents and scope mutation (need new herald endpoints first), `cw whoami` (needs a herald introspection endpoint). With #4 done, the CW CLI suite (#0–#4) covers auth + all four pillars (cairn/ledger/commonplace/herald-admin) — `cw` can self-provision the platform.
