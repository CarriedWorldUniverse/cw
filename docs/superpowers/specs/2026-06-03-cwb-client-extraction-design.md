# `cwb-client` extraction — design

**Date:** 2026-06-03
**Status:** design (approved in brainstorming)
**Scope:** lift cw's proven pillar/auth client out of `internal/` into a standalone, reusable Go module (`github.com/CarriedWorldUniverse/cwb-client`) so nexus, outposts, and aspects import the *same* live-proven code. Two-repo cycle: create the module, then repoint cw to consume it. Step 0 of the herald-rooted agent bootstrap (`nexus/docs/2026-06-03-herald-rooted-agent-bootstrap-design.md`).

## Goal

The cw CLI suite (#0–#9) proved a complete CWB client — auth (`oidc`/`identity`), the HTTP seam (`client`), and the four pillar wrappers (`herald`/`ledger`/`cairn`/`commonplace`). They're locked in `cw/internal/`, so nexus (a different Go module) can't import them. Extract them into a dedicated module with a clean dependency footprint (`casket-go` + `go-jose` + stdlib — no cobra/keyring/term) so the bootstrap (nexus as token custodian) stands on the proven code instead of reimplementing four HTTP clients.

## Grounding (cw's internal import graph, verified)

- `cairn`/`commonplace`/`herald`/`ledger` → depend only on `client`.
- `client` → imports `oidc` + `tokenstore` (+ `go-keyring`) for silent refresh — **the coupling to fix.**
- `oidc` → no internal deps (clean). `identity` → no internal deps; external `casket-go`+`go-jose`+`golang.org/x/term`.
- `tokenstore` → `config`; `cmdutil` → `client`+`config`+`oidc`+`tokenstore` (the CLI session builder).

So the wrappers are clean leaves; the only real coupling is `client`→`tokenstore`/`config`/`go-keyring` (CLI persistence), and two interactive functions inside `identity`.

## The module boundary

| → `cwb-client` (lib) | stays in `cw` (CLI) |
|---|---|
| `client` (TokenSource-parameterized) | `config`, `tokenstore` (keychain) |
| `oidc` | `session` (new — builds the client w/ a tokenstore TokenSource) / `cmdutil` |
| `identity` (assertion/fingerprint/claims) | the interactive prompts (`PromptHuman`/`PromptPassword`) |
| `herald` `ledger` `cairn` `commonplace` | `cli/*`, `cmd/cw` |

Lib external deps: `casket-go`, `go-jose`, stdlib. (`identity`'s `golang.org/x/term` prompts do NOT move.)

## The load-bearing refactor — `TokenSource` inversion

Today `client` hard-wires silent refresh to the concrete `tokenstore`+`oidc`. Replace that dependency with an interface so the seam is reusable by any token holder:

```go
// TokenSource supplies (and refreshes) the bearer the client presents.
type TokenSource interface {
	// Token returns the current access token, refreshing if stale.
	Token(ctx context.Context) (string, error)
	// Refresh forces a refresh (called after a 401); returns the new token,
	// or ErrReauth if re-auth is required.
	Refresh(ctx context.Context) (string, error)
}
```

- `client` holds a `TokenSource`: `Do` calls `Token()` for the bearer; on a 401 it calls `Refresh()` then retries once; `ErrReauth` propagates. `client` no longer imports `tokenstore`/`oidc`/`go-keyring` — only stdlib + this interface.
- `WithStaticToken(edge, token)` becomes a trivial static `TokenSource` (`Token` returns the fixed token; `Refresh` returns `ErrReauth`).
- **cw** provides a `tokenstore`+`oidc`-backed `TokenSource` (the current refresh-grant + keychain-save logic, relocated into a cw `session` package). The CLI behavior is unchanged — the refresh *policy* just moved out of `client` into cw's impl.
- **nexus** (later) provides a custodian-backed `TokenSource` (holds the per-aspect herald token; `Refresh` re-redeems the casket assertion).

This is the one real design change, and it's a genuine improvement — it decouples the HTTP seam from CLI persistence, which is exactly what makes it shareable.

## `identity` split

The lib's `identity` keeps the crypto/claims: `AgentAssertion`, `AgentAssertionAt`, `Fingerprint`, `DecodeAccessClaims` (deps `casket-go`+`go-jose`). The interactive `PromptHuman`/`PromptPassword` (deps `golang.org/x/term`) stay in cw (a small cw-side `prompt` package or in `cli/auth`). cw's agent-assertion + claims usage repoints to the lib; its prompt usage stays local.

## Build order

1. **cwb-client module** (new repo `github.com/CarriedWorldUniverse/cwb-client`): `go mod init`, move the seven packages in, apply the `TokenSource` inversion (+ the static source) and the `identity` split, port their unit tests (httptest stubs — they need no CLI deps), `go build`/`go test`/`go vet` green. Minimal CI mirroring the other CWU repos. Merge → pinned pseudo-version `H` (merged-main hash, per the #7a rule).
2. **cw** consumes it: `go get github.com/CarriedWorldUniverse/cwb-client@H` + `go mod tidy`; repoint every import (`cw/internal/{client,oidc,identity,herald,ledger,cairn,commonplace}` → `cwb-client/...`); implement the tokenstore-backed `TokenSource` in a cw `session` package and pass it to `client.New`; keep `config`/`tokenstore`/the prompts; delete the moved `internal/` packages. `go build`/`go test`/`go vet` green.

## Error handling / behavior

Pure refactor — **no wire-behavior change.** `ErrReauth`, the 401-refresh-retry, the JSON Content-Type, url-escaping, the output convention, every command — all byte-for-byte identical. The `TokenSource` inversion preserves the exact refresh semantics; cw's impl is the relocated current logic.

## Testing

- **cwb-client:** the moved unit tests (the existing httptest-stub wrapper tests + the client/oidc/identity tests) pass in the new module unchanged (they had no CLI deps). Add a small `TokenSource` test for the static source + the 401-refresh path.
- **cw:** the full existing suite stays green after repointing — that's the regression guard (every cli/* wiring test, every wrapper test exercised via the new import). `go build ./... && go test ./... && go vet ./...`.
- **Live smoke (dMon):** one end-to-end — `cw auth login --agent` (refresh + assertion path through the new lib) + a pillar op (e.g. `cw whoami --remote` or `cw kb list`) — proving the extraction changed no behavior against the live platform.

## Out of scope

nexus consuming the lib (the next bootstrap step); any new client capability; any wire/behavior change; GUI (CLI-first principle).

## References

`cw/docs/superpowers/` (#0–#9 specs/plans); `nexus/docs/2026-06-03-herald-rooted-agent-bootstrap-design.md` (step 0); the #7a multi-repo pseudo-version rule (merge upstream → pin merged-main hash downstream).
