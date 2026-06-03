# `cw whoami --remote` — design

**Date:** 2026-06-03
**Status:** design (approved in brainstorming)
**Sub-project:** #7b of the server-authoritative-whoami feature — the cw client side. Consumes herald's `GET /api/me` (shipped + live on dMon in #7a). Single cw-side cycle on the existing `internal/herald` wrapper + `internal/cli/auth` whoami.

## Goal

Let `cw whoami --remote` fetch the server-authoritative identity record (the superset the local token+config view can't know — status, org name, store-current scopes, and an agent's responsible human + casket fingerprint), while the default `cw whoami` stays the fast offline view from #6.

## Grounding (verified live + in the cw codebase)

- herald `GET /api/me` (through the gateway `<edge>/herald/api/me`, any authenticated bearer) returns a **bare** `UserInfo{id, kind, display_name, org, org_name, status, scopes[], responsible_human, fingerprint}` (snake_case; agent fields empty for humans). Confirmed live: `curl` as cwadmin returned `display_name`+`org_name` (not in the token); unauth → 401.
- `internal/herald/herald.go`: the typed wrapper with `do(ctx, c, method, path, body, out)` / `errMsg`, pillar `"herald"`, `client.ErrReauth` pass-through — the same shape used by `CreateOrg`/`CreateHuman`/`CreateAgent`/etc.
- `internal/cli/auth/whoami.go` (from #6): `Info{Context,Edge,Kind,Subject,Display,Slug,Org,Scopes[],Products[],ExpiresIn}`; `whoamiInfo(gf)` calls `session(gf)` (→ `*client.Client`, `config.Context`, name) then decodes the local token claims; `NewWhoamiCmd(gf)` is the command (registered top-level `cw whoami` + as `cw auth whoami`). No import cycle: `auth → herald → client` is acyclic (`internal/herald` does not import `internal/cli/auth`).

## Scope

**In:** `internal/herald` `UserInfo` type + `Me(ctx, c)` wrapper; a `--remote` flag on `cw whoami` rendering the authoritative record; tests + README note.

**Out:** changing the default (offline) whoami; caching the remote result; OIDC userinfo; any new herald work (the endpoint is already live).

## Tech

Go 1.26, cobra. Reuses `internal/herald` + `internal/cli/auth` (`session`). No new deps.

---

## `internal/herald` — `UserInfo` + `Me`

```go
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

func Me(ctx context.Context, c *client.Client) (UserInfo, error) // GET /api/me -> bare UserInfo
```

`Me` mirrors the existing getters: `do(ctx, c, http.MethodGet, "/api/me", nil, &ui)` (pillar `"herald"`). Non-2xx → `errMsg` (e.g. 401 surfaced); `client.ErrReauth` passes through.

## `cw whoami --remote`

Add a command-local `--remote` bool to `NewWhoamiCmd(gf)`. The RunE branches:

- **default (no `--remote`)** — unchanged #6 path: `whoamiInfo(gf)` → the local context/edge/kind/subject/display/slug/org/scopes/products/expires view (or `--json` of `Info`).
- **`--remote`** — obtain the session client (the same `session(gf)` the local path uses), call `herald.Me(ctx, c)`, then render the authoritative record. Reads the local context name + edge for orientation (from `session`'s returned `config.Context`).

Text layout (`--remote`):
```
context:  default
edge:     http://<edge>
id:       <id>
kind:     <human|agent>
display:  <display_name>
org:      <org-id>  (<org_name>)
status:   <status>
scopes:   <space-joined authoritative scopes>
responsible_human: <id>     # printed only when non-empty (agent)
fingerprint:       <fp>     # printed only when non-empty (agent)
```
`org` shows `id (org_name)`; the agent-only lines are omitted when empty (human). `--remote --json` emits the bare `herald.UserInfo` to stdout. Confirmation/orientation (context/edge) to stdout alongside the record (this is a query command — all output to stdout; no stderr side-effect).

A successful `--remote` doubles as a server-side token check: an expired/invalid/revoked token yields herald's error (or `ErrReauth` → the root "session expired") rather than the offline decode's silent `expires: expired`.

## Data flow

`cw whoami --remote` → `session(gf)` (client + context) → `herald.Me(ctx, c)` → `GET <edge>/herald/api/me` (bearer) → bare `UserInfo` → render (text or `--json`).

## Error handling

- `--remote` + expired/invalid token → herald's non-2xx via `errMsg`, or `client.ErrReauth` → "session expired" (the root handler). The default offline path is unaffected and still works when herald is unreachable.
- not logged in (no context/token) → the existing `session(gf)` error, same as the default path.

## Testing

- `internal/herald`: extend the wrapper test — `Me` decodes a bare `UserInfo` (human: empty agent fields; agent: responsible_human + fingerprint set); a non-2xx (401) error mapping case.
- `internal/cli/auth/whoami_test.go`: a `--remote` wiring test — stub `/herald/api/me`, run `cw whoami --remote` through cobra `Execute` against the stub (static `--token`), assert the endpoint is hit and the server fields render (human omits the agent lines; agent includes them); a `--remote --json` test asserting the emitted JSON is the `UserInfo`. The existing default-path tests stay green (no behavior change without the flag).
- Gated live integration (`CW_IT_*`, skips offline): as a provisioned agent (the same provisioning the #5 live test uses), `cw whoami --remote`/`herald.Me` returns `kind=="agent"`, non-empty `responsible_human` + `fingerprint`, `status=="active"`. (Reuses the `liveSession` helper pattern.)
- README: a line under the Identity section for `cw whoami --remote`.

## Build order

Single cycle: `internal/herald.Me` + `UserInfo` (Task 1) → `cw whoami --remote` flag + rendering + register/tests (Task 2) → README + gated live test (Task 2 or a small Task 3).

## Future (deferred)

`cw whoami` could later default to `--remote`-with-local-fallback if the offline path proves rarely wanted; OIDC userinfo; caching. Not now.
