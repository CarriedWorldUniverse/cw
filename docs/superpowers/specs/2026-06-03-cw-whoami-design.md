# `cw whoami` — design

**Date:** 2026-06-03
**Status:** design (approved in brainstorming)
**Sub-project:** #6 of the CW CLI suite — a top-level, enriched `cw whoami`. Entirely client-side (no herald change, no network beyond the token refresh the path already does). Builds on the existing `internal/cli/auth` `whoamiInfo`.

## Goal

Give a complete picture of the current identity by merging the **token claims** (ids, scopes, products, expiry) with the **config context** (human-readable display name, agent slug, edge URL) that the current `cw auth whoami` omits — and surface it at a discoverable top-level `cw whoami`.

## Grounding (what exists today)

`internal/cli/auth/whoami.go` already has:
- `Info{Context, Subject, Kind, Org, Scopes[], Products[], ExpiresIn}` (json-tagged).
- `whoamiInfo(gf)` — calls `session(gf)` (returns `(*client.Client, config.Context, name string, error)`; the `config.Context` is currently discarded with `_`), gets a fresh access token, decodes its claims via `identity.DecodeAccessClaims`, and fills `Info` from `sub`/`kind`/`org`/`scope`/`products`/`exp`. No other network call.
- `newWhoamiCmd(gf)` — prints the multi-line text or `--json`, registered under `cw auth` (via the auth group's `NewCmd`).
- `cw auth status` (separate) lists contexts + token state.

The access token carries only **ids** (`sub`, `org`) + `kind`/`scope`/`products`/`exp`. The human-readable **display name**, agent **slug**, and **edge** live in the config context (`config.Context{Edge, Identity{Kind,Subject,Display,Slug,Org}}`), populated at login — NOT in the token. There is no herald `/userinfo` endpoint, so authoritative server-side identity (responsible-human, fingerprint) is out of reach without new herald work (declined).

## Scope

**In:** enrich `Info` + `whoamiInfo` with `Edge`/`Display`/`Slug` from the config context already returned by `session(gf)`; update the text output; register the SAME command factory at top-level `cw whoami` (keeping `cw auth whoami` as the alias).

**Out:** a server-authoritative `/userinfo` (needs new herald endpoints); `--json` for `cw auth status`; any new network call; touching the token-claim decode logic.

## Tech

Go 1.26, cobra. No new deps, no new package — the change is confined to `internal/cli/auth/whoami.go` + the registration in `cmd/cw/main.go`.

---

## Enriched `Info` + `whoamiInfo`

Add three config-sourced fields:

```go
type Info struct {
	Context   string   `json:"context"`
	Edge      string   `json:"edge"`
	Kind      string   `json:"kind"`
	Subject   string   `json:"subject"`
	Display   string   `json:"display,omitempty"`
	Slug      string   `json:"slug,omitempty"`
	Org       string   `json:"org"`
	Scopes    []string `json:"scopes"`
	Products  []string `json:"products"`
	ExpiresIn int      `json:"expires_in_seconds"`
}
```

`whoamiInfo` captures the context that `session(gf)` already returns (today discarded) and fills the new fields from it:
- `Edge` ← `ctx.Edge` (the resolved edge, including a `--edge` override since `session` resolves it).
- `Display` ← `ctx.Identity.Display`.
- `Slug` ← `ctx.Identity.Slug` (empty for humans).

The token-claim decode (subject/kind/org/scopes/products/expires) is unchanged. Under the stateless `--token` path (no stored context) the config-sourced fields are simply blank — print what's available; do not error.

## Text output

Order matches the approved layout (config + claims interleaved for readability); the `slug` line appears only when non-empty (agent-only):

```
context:  default
edge:     http://dmonextreme.tail41686e.ts.net:8080
kind:     agent
subject:  9da61560-...
display:  builder
slug:     builder
org:      9fb90a95-...
scopes:   repo:read repo:write
products: cairn ledger commonplace
expires:  540s
```

`expires` stays `<N>s` or `expired` (when `ExpiresIn <= 0`). `--json` encodes the enriched `Info` (slug/display omitted via `omitempty` when empty).

## Top-level registration

Export the factory (`newWhoamiCmd` → `NewWhoamiCmd`) and register it at the root in `cmd/cw/main.go` (`root.AddCommand(auth.NewWhoamiCmd(flags))`), in addition to its existing registration under the `auth` group. Both are the same factory → identical behavior; `cw whoami` is the discoverable top-level, `cw auth whoami` remains as the alias.

## Error handling

Unchanged: no current context / no token → the existing not-logged-in error from `session(gf)`; an expired token still decodes and shows `expires: expired`.

## Testing

- `internal/cli/auth/whoami_test.go` (extend): with a written config context + a stubbed token, assert `Edge`/`Display`/`Slug` populate; the agent case prints a `slug:` line and the human case omits it; `--json` includes `edge`/`display` and omits empty `slug`. Keep the existing claim-decode assertions.
- A top-level wiring test: `cw whoami` (root command) executes and yields the same `Info`/output as `cw auth whoami`.
- README: a short `## whoami` note (or fold into the auth usage section) showing `cw whoami` + the enriched fields.

## Build order

Single small cycle: enrich `Info`/`whoamiInfo` + text output (Task 1) → export factory + register top-level + tests + README (Task 2).

## Future (deferred)

Server-authoritative `cw whoami` via a herald `/userinfo` (or `GET /api/me`) endpoint — would add responsible-human/fingerprint + confirm the token works server-side, but needs new herald work. `--json` for `cw auth status`. With #6, the CW CLI suite covers identity introspection end to end on the client side.
