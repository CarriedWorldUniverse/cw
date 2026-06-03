# `cw agent pubkey` — design

**Date:** 2026-06-03
**Status:** design (approved in brainstorming)
**Sub-project:** #9 of the CW CLI suite — the deferred local, no-network agent key inspect helper. cw-only.

## Goal

`cw agent pubkey --slug <slug>` derives the agent keypair from `CW_OWNER_SEED` + slug (the SAME derivation `cw agent create` / `cw auth login --agent` use) and prints the **base64-std public key** + the **casket fingerprint** — purely locally. Two uses: inspect what `cw agent create` would register, and verify a local `CW_OWNER_SEED`+slug derives an already-registered agent by comparing the fingerprint to `cw whoami --remote` / `/api/me` — read-only, no write access needed.

## Grounding (verified)

- Agent key derivation: `casket.DeriveAgentKey([]byte(seed), slug) (ed25519.PrivateKey, ed25519.PublicKey, error)` (HKDF-SHA256; deterministic). `cw agent create` already does `base64.StdEncoding.EncodeToString(pub)` as `casket_pubkey`.
- **Fingerprint** is herald's convention, NOT casket-go's: herald `internal/identity/fingerprint.go` — `Fingerprint(pub) = base64.RawURLEncoding(sha256(pub)[:16])` (16 bytes / 128 bits). The comment states herald owns this convention ("casket-go has no fingerprint convention of its own ... if casket adopts one later, align here"). cw must replicate it to display a fingerprint that matches herald's stored value + what `/api/me` returns (confirmed live: e.g. `Lc7SJ_7csJp7ftIFWcH59w`, 22 base64url chars).
- `cw agent` group lives in `internal/cli/agent/agent.go` (`keygen` + `create`); `create` reads `seed := os.Getenv("CW_OWNER_SEED")` (raw, not base64-decoded) and derives. `internal/identity` is where cw's casket usage lives (`AgentAssertion` → `casket.DeriveAgentKey`).

## Scope

**In:** `identity.Fingerprint(pub)` in cw; `cw agent pubkey --slug <slug>` (local derive → pubkey + fingerprint, text or `--json`); tests + README.

**Out:** any herald/network call (offline only); registering/rotating keys; emitting the seed or private key.

## Tech

Go 1.26, cobra, `crypto/ed25519`+`crypto/sha256`+`encoding/base64`, `casket-go` (already a dep). No new deps, no session.

---

## `internal/identity.Fingerprint`

Replicate herald's algorithm (documented as the canonical convention to align with):

```go
// Fingerprint is the casket Ed25519 pubkey's stable identifier, matching
// herald's identity.Fingerprint: base64url(sha256(pubkey)[:16]). Deterministic.
// Herald owns this convention (its internal/identity/fingerprint.go); cw mirrors
// it so a locally-derived fingerprint matches herald's stored value + /api/me.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
```

(Placed in `internal/identity` alongside the existing casket helpers.)

## `cw agent pubkey`

A sibling subcommand of `keygen`/`create` in `internal/cli/agent/agent.go`. `NewCmd` adds `newPubkeyCmd()`.

| Command | Behavior |
|---|---|
| `cw agent pubkey --slug <slug>` | `--slug` required (fail-fast). Read `CW_OWNER_SEED` (required; same fail message as `create`: "requires the owner seed in CW_OWNER_SEED"). Derive `_, pub, _ := casket.DeriveAgentKey([]byte(seed), slug)`; `pubB64 := base64.StdEncoding.EncodeToString(pub)`; `fp := identity.Fingerprint(pub)`. Default → labeled lines to `cmd.OutOrStdout()`; `--json` → `{slug, pubkey, fingerprint}`. No session, no network. |

Default text:
```
pubkey:      <base64-std public key>
fingerprint: <base64url fingerprint>
```
`--json` (a small local struct):
```json
{"slug":"builder","pubkey":"<b64-std>","fingerprint":"<b64url>"}
```

`newPubkeyCmd` takes no `gf` for the session, but DOES need `gf.JSON` for the output mode — so it takes `gf *GlobalFlags` like `create` (just never calls `Session`). The seed/private key are never printed.

## Data flow

`cw agent pubkey --slug X` → read `CW_OWNER_SEED` → `casket.DeriveAgentKey` → (pubB64, fingerprint) → text or `--json` to stdout. No herald, no config/token store.

## Error handling

- `--slug` empty → fail-fast.
- `CW_OWNER_SEED` unset → fail-fast (same message style as `create`).
- No network/IO failure modes.

## Testing

- `internal/identity`: `TestFingerprint` — deterministic (same pub → same fp), correct length/charset (base64url of 16 bytes = 22 chars, no padding); a fixed pub → a pinned expected fp (so the algorithm is locked; if it drifts from herald, the gated live test below catches the real mismatch).
- `internal/cli/agent`: `TestPubkey` — fixed `CW_OWNER_SEED`+slug via `t.Setenv`, run `cw agent pubkey --slug X` through cobra `Execute` with `cmd.SetOut(&buf)`; assert the printed pubkey == `base64.StdEncoding(DeriveAgentKey([]byte(seed),slug).pub)` and the fingerprint == `identity.Fingerprint(pub)` (recompute in-test); a `--json` decode test; a missing-`CW_OWNER_SEED` fail-fast test (and missing-`--slug`).
- Gated live integration (the drift guard, `CW_IT_*` skips offline): provision an agent via cw (owner seed + slug + the registered agent id), then assert `identity.Fingerprint(DeriveAgentKey([]byte(seed), slug).pub)` **==** `herald.Me(...).Fingerprint` fetched from the live endpoint as that agent. This proves cw's local fingerprint matches herald's stored value end-to-end.
- README: a `cw agent pubkey` line under `## Agents (herald admin)` noting the offline derive + the verify-vs-`cw whoami --remote` use.

## Build order

Single cycle: `identity.Fingerprint` + unit (Task 1) → `cw agent pubkey` command + unit tests + register (Task 2) → README + gated live drift-guard test (Task 3).

## Future (deferred)

None specific. (Could add `--quiet` to print only the pubkey for scripting, but `--json | jq` covers it — YAGNI.)
