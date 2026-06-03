# `cw kb update` / `cw kb delete` — design

**Date:** 2026-06-03
**Status:** design (approved in brainstorming — "#10 looks right")
**Sub-project:** #10 — complete the `cw kb` CRUD over commonplace's already-live `Update`/`Delete`. **Now a two-repo cycle** (the `commonplace` wrapper moved to the `cwb-client` module in the extraction): add the wrappers to `cwb-client`, then cw bumps + adds the commands. First instance of the post-extraction pattern.

## Goal

Give `cw kb` the missing edit/remove verbs. commonplace exposes `Update` (`PATCH /api/knowledge/{id}` → bare `Entry`) and `Delete` (`DELETE /api/knowledge/{id}` → 204); both are routed by the deployed gateway today (the interchange pinned to the same `c76ceb4` already has the HTTP bindings), both require `knowledge:write` + are **owner-scoped**. No proto/interchange/deploy work.

## Platform grounding (verified)

- **Update** — `PATCH /api/knowledge/{id}`, `body:"*"`, `response_body:"entry"` → bare `Entry`. Request `{id, topic, content, visibility, tags[]}`; **partial/field-mask** — the server applies only non-empty/supplied fields (empty = leave unchanged); topic/content changes trigger re-embedding (search stays consistent). Owner-enforced (`ownedEntry` → `Forbidden` if not owner), `knowledge:write`.
- **Delete** — `DELETE /api/knowledge/{id}` → empty 204. **Hard-delete** (removes entry + FTS + vector rows), owner-enforced, `knowledge:write`.
- The id comes from `cw kb search`/`list` output (`Entry.ID`).
- `cwb-client/commonplace` already has `Store`/`Search`/`List` + the `do(ctx,c,method,path,body,out)` helper (pillar `knowledge`, `base = "/api/knowledge"`, imports `net/url`) + `errMsg`. `client.Do` takes the method as a string → PATCH/DELETE work; `do` with `out=nil` is a 2xx-only check (204 passes).

## Scope

**In:** `cwb-client/commonplace` `UpdateInput` + `Update` + `Delete`; `cw kb update`/`delete`; tests + README.

**Out:** appending tags (it's a full-field replace); `--content-stdin` for update; soft-delete/undo (server hard-deletes); any proto/interchange change.

## Tech

Go 1.26. `cwb-client/commonplace` (lib) + `cw/internal/cli/kb` (commands). cw bumps its `cwb-client` pin.

---

## `cwb-client/commonplace` — `Update` + `Delete`

```go
// UpdateInput patches a knowledge entry. Only non-nil fields are sent; the
// server leaves unsupplied fields unchanged. Tags is a full replace.
type UpdateInput struct {
	Topic      *string   `json:"topic,omitempty"`
	Content    *string   `json:"content,omitempty"`
	Visibility *string   `json:"visibility,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
}

func Update(ctx, c *client.Client, id string, in UpdateInput) (Entry, error) // PATCH /api/knowledge/{id} -> bare Entry
func Delete(ctx, c *client.Client, id string) error                          // DELETE /api/knowledge/{id} -> 2xx-only (204)
```

- `Update`: `do(ctx, c, http.MethodPatch, base+"/"+url.PathEscape(id), in, &e)` → bare `Entry` (the updated record). Only the pointer fields that are non-nil marshal into the body (the command sets them only for flags the user changed).
- `Delete`: `do(ctx, c, http.MethodDelete, base+"/"+url.PathEscape(id), nil, nil)` (2xx-only; 204 passes the `do` non-2xx check, `out=nil` skips decode).
- Mirrors the existing `Store`/`Search`/`List` idiom; `client.ErrReauth` passes through; non-2xx → commonplace's message (404 not-found, 403 not-owner / missing scope).

Merge `cwb-client` → pin hash `H2`; cw bumps `go get cwb-client@H2`.

## `cw kb update` / `cw kb delete` (`internal/cli/kb`)

| Command | Behavior |
|---|---|
| `cw kb update <id> [--topic <t>] [--content <c>] [--visibility org\|private] [--tag <x> ...]` | `ExactArgs(1)`. Build `UpdateInput` from the flags actually set (`cmd.Flags().Changed(...)` → set the pointer); **fail-fast if none set** ("nothing to update — set --topic/--content/--visibility/--tag"). `--tag` is repeatable (`StringArrayVar`) and **replaces** the entry's tags (full-field patch — noted in `--help`). `commonplace.Update`. Confirmation → stderr; `--json` → the updated `Entry` → stdout. |
| `cw kb delete <id> --yes` | `ExactArgs(1)`. `--yes` **required** (fail-fast "pass --yes to confirm deletion" — irreversible hard-delete). `commonplace.Delete`. `deleted <id>` → stderr. |

Output convention unchanged (side-effect → stderr; `--json` → stdout). Content via `--content` flag only (store keeps the stdin/from-doc path; update is for tweaks).

## Data flows

- **update:** `Session` → build `UpdateInput` from changed flags → `commonplace.Update(id, in)` → updated `Entry` → confirm.
- **delete:** `Session` → require `--yes` → `commonplace.Delete(id)` → confirm.

## Error handling

- non-2xx → commonplace's message: `missing scope knowledge:write` (403), not-owner (403/Forbidden), not-found (404); `client.ErrReauth` → "session expired".
- `update` with no changed flags / `delete` without `--yes` → fail-fast client-side.

## Testing

- `cwb-client/commonplace`: `Update` (PATCH, bare-`Entry` decode, body carries only the set fields — assert via the stub), `Delete` (DELETE, 2xx-only on 204), a 404 error case.
- `cw/internal/cli/kb`: update wiring (changed flags → PATCH body), update-nothing-to-change fail-fast, delete wiring, delete-requires-`--yes` fail-fast.
- Gated live (reuses the #3 `knowledge:*` identity): store → update its topic → confirm the change (list/search) → delete → confirm it's gone (list no longer contains the id). Full CRUD round-trip.
- README: `cw kb update`/`delete` lines under Knowledge.

## Build order

1. `cwb-client/commonplace`: add `UpdateInput` + `Update` + `Delete` + unit tests; merge → `H2`.
2. cw: `go get cwb-client@H2`; add `cw kb update`/`delete` + tests + README; live CRUD smoke + merge.

## References

`cw/docs/superpowers/specs/2026-06-02-cw-kb-design.md` (#3); the cwb-client extraction (#wrappers now in the lib); the #7a cross-repo pin rule (merge lib → `go get @merged-hash`).
