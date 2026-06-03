# `--json` for `cw auth status` — design

**Date:** 2026-06-03
**Status:** design (approved in brainstorming)
**Sub-project:** #8 of the CW CLI suite — make `cw auth status` honor the global `--json` flag. Small, single-file, cw-only.

## Goal

`cw auth status` lists contexts + token freshness as a human table only; scripts can't consume it. Add structured `--json` output (the global `--json` flag already exists; status just needs to honor it), and make the listing deterministically ordered.

## Grounding (current code)

`internal/cli/auth/status.go` — `newStatusCmd` loads `config.Load()`, prints a `* name edge display (state)` line per context (map iteration order, non-deterministic), where `state` ∈ {valid, refreshable, logged-out} computed from `tokenstore.New(edge, name, subject)` (cached access live → valid; refresh present → refreshable; else logged-out). The empty case prints a "no contexts" hint. `config.Context{Edge, Identity{Kind,Subject,Display,Slug,Org}}`; `cfg.CurrentContext`. `GlobalFlags.JSON` is the shared flag whoami already honors.

## Scope

**In:** a `statusEntry` type; build a sorted `[]statusEntry`; branch on `gf.JSON` (encode the array, `[]` when empty) vs the existing text table (now sorted); render via `cmd.OutOrStdout()`.

**Out:** new fields beyond config/token state already available; changing the state semantics; touching whoami.

## Tech

Go 1.26, cobra. `sort`, `encoding/json`. No new deps.

---

## `statusEntry` + `newStatusCmd`

```go
type statusEntry struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`            // name == cfg.CurrentContext
	Edge    string `json:"edge"`
	Kind    string `json:"kind,omitempty"`     // ctx.Identity.Kind
	Display string `json:"display,omitempty"`  // ctx.Identity.Display
	Subject string `json:"subject,omitempty"`  // ctx.Identity.Subject
	Org     string `json:"org,omitempty"`      // ctx.Identity.Org
	State   string `json:"state"`              // valid | refreshable | logged-out
}
```

`newStatusCmd` RunE:
1. `config.Load()` (propagate error).
2. Collect context names, `sort.Strings`. For each (in sorted order), compute `state` (the existing tokenstore logic) and build a `statusEntry` (`Current = name == cfg.CurrentContext`).
3. **`gf.JSON`** → `json.NewEncoder(cmd.OutOrStdout()).Encode(entries)` (where `entries` is `[]statusEntry`, an empty non-nil slice `[]statusEntry{}` so the JSON is `[]` not `null` when there are no contexts).
4. **default text** → if no contexts, the existing "no contexts (run 'cw auth login --edge <url>')" hint; else the existing `%s%-12s %-28s %s (%s)\n` table (marker/name/edge/display/state) in the sorted order, via `cmd.OutOrStdout()`.

The `state` computation, marker (`* ` current / `  ` other), and the text format string are unchanged — only the ordering becomes deterministic and a `--json` branch is added.

## Data flow

`cw auth status [--json]` → `config.Load()` → per-context state via `tokenstore` → sorted `[]statusEntry` → text table (default) or JSON array (`--json`) to stdout.

## Error handling

`config.Load()` error → propagated. Per-context token reads stay best-effort (an unreadable token → `logged-out`, as today). No network.

## Testing

`internal/cli/auth/status_test.go` (new or extend): seed two contexts (e.g. `dev` current + `prod`) with cached access tokens via `keyring.MockInit()` + `tokenstore.New(...).SaveAccess(...)` (the pattern `whoami_test.go` uses), run `cw auth status --json` through cobra `Execute` with `cmd.SetOut(&buf)`, decode the JSON array, assert: sorted by name, `current` true on `dev` only, `state=="valid"`, the structured fields (kind/subject/edge) populate. A default-text test asserting the sorted table + `*` marker and the empty-contexts JSON case (`[]`). Use `GlobalFlags{JSON: true}` for the json path.

## Build order

Single small cycle: the `statusEntry` + sorted build + `--json` branch + tests (one task). Optional README touch if `cw auth status` has a usage mention (add `--json`).

## Future (deferred)

None specific. (`cw auth status` could later show expiry seconds per context, but that's out of scope.)
