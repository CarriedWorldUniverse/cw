# `cw agent` — design

**Date:** 2026-06-03
**Status:** design (approved in brainstorming)
**Sub-project:** #5 of the CW CLI suite — agent identity creation. The deferred piece from #4. Single cw-side cycle (herald already exposes `CreateAgent`; only the wrapper function was held back). Builds on the #0b `internal/client` seam + the #4 `internal/herald` wrapper + the existing `internal/identity` agent-login path + `casket-go` (already a dependency).

## Goal

Let `cw` create agent identities, completing the suite's self-provisioning story (after #5, `cw` covers auth + all four pillars + org/human/agent admin). An agent is a herald identity with a deterministic ed25519 "casket" key derived from an owner seed + slug, linked to a responsible human.

## Platform grounding (the key model)

Agent identity in `cw` is a **deterministic derivation**, established by the existing login path:

- `cw auth login --agent` (`internal/cli/auth/login.go`) reads `CW_OWNER_SEED` (as raw string bytes: `[]byte(os.Getenv("CW_OWNER_SEED"))`), `--agent-id`/`CW_AGENT_ID`, `--slug`/`CW_AGENT_SLUG`, then signs an RFC 7523 assertion with the key from `casket.DeriveAgentKey(seed, slug)` and exchanges it via `oidc.JWTBearerGrant` (`grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer`).
- `casket.DeriveAgentKey(seed []byte, slug string) (ed25519.PrivateKey, ed25519.PublicKey, error)` is **deterministic**: HKDF-SHA256(IKM=seed, salt=nil, info=`"cairn-agent-v1:"+slug`) → 32-byte seed → `ed25519.NewKeyFromSeed`. Same `(seed, slug)` → same keypair, anywhere. Different slugs → independent keys under one owner seed.
- The conformance fixture creates agents this exact way: derive `(priv, pub)`, register `base64.StdEncoding(pub)` as `casket_pubkey`, later mint a token by signing with the same derived key.

So **`cw agent create` derives the public key from `(CW_OWNER_SEED, slug)` and registers it** — there is no new private key to generate or store; `cw auth login --agent` re-derives the signing key from the same seed+slug. The only invariant: `create` and `login` treat `CW_OWNER_SEED` identically (both as raw string bytes), which they will (create reuses the same seed-load as login).

Herald's `CreateAgent` (through the gateway `/herald`, org-admin/platform-admin):
- `POST /api/orgs/{org}/agents`, body `{display_name, responsible_human, casket_pubkey, scopes[]}` (`org` in path only) → **bare `Agent`** (`response_body:"agent"`).
- `Agent{id, kind, display_name, org, responsible_human, fingerprint, status, active, scopes[]}`. snake_case JSON. `casket_pubkey` = base64-std of the 32-byte ed25519 public key. `responsible_human` must be a human id in the same org; duplicate fingerprints are rejected.

## Scope

**In:** `internal/herald.CreateAgent` (the one deferred wrapper func) + `cw agent keygen` + `cw agent create`.

**Out (future / no server endpoint):** list/get/delete agents, scope mutation, key rotation/revocation (herald exposes none); storing the owner seed (stays in `CW_OWNER_SEED`, same as login); a no-network `cw agent pubkey` derive helper (deferred — not needed for the create loop).

## Tech

Go 1.26; reuses `internal/client`, `internal/cmdutil` (`Session`), `internal/herald`, `casket-go` (`DeriveAgentKey`), `crypto/rand` + `encoding/base64` (keygen). No proto/herald change, no new deps.

---

## `internal/herald` — add `CreateAgent`

Mirrors the existing wrapper funcs (`do`/`errMsg`, pillar `"herald"`, `url.PathEscape` on the org segment).

```go
type Agent struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	DisplayName      string   `json:"display_name"`
	Org              string   `json:"org"`
	ResponsibleHuman string   `json:"responsible_human"`
	Fingerprint      string   `json:"fingerprint"`
	Status           string   `json:"status"`
	Active           bool     `json:"active"`
	Scopes           []string `json:"scopes"`
}
type CreateAgentInput struct {
	DisplayName      string   `json:"display_name"`
	ResponsibleHuman string   `json:"responsible_human"`
	CasketPubkey     string   `json:"casket_pubkey"`
	Scopes           []string `json:"scopes,omitempty"`
}

func CreateAgent(ctx, c *client.Client, org string, in CreateAgentInput) (Agent, error) // POST /api/orgs/{org}/agents -> bare Agent
```

`org` is in the path (escaped), not the body — same shape as `CreateHuman`.

## `internal/cli/agent` — commands (on `cmdutil.Session` + `CW_OWNER_SEED`)

`NewCmd(*cmdutil.GlobalFlags)` → keygen + create. Registered in `cmd/cw/main.go` after `human`.

| Command | Behavior |
|---|---|
| `cw agent keygen` | Generate 32 random bytes (`crypto/rand`), print `base64.StdEncoding` of them to **stdout** as the owner-seed string; a one-line note to **stderr** ("set this as CW_OWNER_SEED; agents under it differ by --slug"). No session, no network. |
| `cw agent create --org <org> --name <dn> --slug <slug> --responsible-human <human-id> [--scope S ...]` | `--org`/`--name`/`--slug`/`--responsible-human` required (fail-fast). Read `CW_OWNER_SEED` (required; same fail message as login). Derive `_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)`; `pubB64 := base64.StdEncoding.EncodeToString(pub)`. `herald.CreateAgent(org, {DisplayName, ResponsibleHuman, CasketPubkey:pubB64, Scopes})`. Print agent id → **stdout**; confirmation + the ready-to-run login invocation → **stderr**; `--json` → the full `Agent` to stdout. |

**Seed handling parity:** `create` loads the seed exactly as login does — `seed := os.Getenv("CW_OWNER_SEED")`, error "agent create requires the owner seed in CW_OWNER_SEED" if empty, then `[]byte(seed)`. This guarantees the registered pubkey matches what `cw auth login --agent` will derive and sign with. (If `internal/cli/auth` exposes its `CW_OWNER_SEED` loader reusably, reuse it; otherwise replicate the two lines — do not base64-decode the env value.)

**Login-hint output** (to stderr, after a successful create):
```
created agent <id> (<slug>) in org <org>; responsible human <human-id>
log in as it with:
  CW_OWNER_SEED=<the same seed> CW_AGENT_ID=<id> CW_AGENT_SLUG=<slug> cw auth login --agent --edge <edge>
```
The `<edge>` is the resolved context/flag edge (from `cmdutil.Session`'s returned context). The seed is NOT echoed in full if that risks leaking it into logs — print a placeholder `$CW_OWNER_SEED` and reference the env var by name rather than its value.

## Data flows

- **stand up a new agent end to end:** `SEED=$(cw agent keygen)` → `cw agent create --org O --name builder --slug builder --responsible-human H --scope repo:read --scope repo:write` (with `CW_OWNER_SEED=$SEED`) → prints agent id `A` → `CW_OWNER_SEED=$SEED CW_AGENT_ID=$A CW_AGENT_SLUG=builder cw auth login --agent` → working agent token.
- **create:** `Session` (admin bearer) + read `CW_OWNER_SEED` → `DeriveAgentKey` → `CreateAgent` → id + login hint.

## Error handling

- `CW_OWNER_SEED` unset → fail-fast before any call (same message style as login).
- missing required flag → fail-fast.
- herald: 403 (`requires herald:org-admin`/`platform-admin`), responsible-human-not-in-org, duplicate fingerprint → surfaced via the wrapper `errMsg`. `client.ErrReauth` → root's "session expired".

## Testing

- `internal/herald`: extend `TestWrapper` (or add a case) — `CreateAgent` POST `/api/orgs/o1/agents`, decode a bare `Agent`; assert the request body carries `display_name`/`responsible_human`/`casket_pubkey`/`scopes` and NO `org`.
- `internal/cli/agent`:
  - `keygen` test: output is valid `base64.StdEncoding`, decodes to exactly 32 bytes; two invocations differ (entropy).
  - `create` wiring test: set `CW_OWNER_SEED` to a fixed string, stub herald, run through cobra `Execute`; assert the POSTed `casket_pubkey` equals `base64.StdEncoding(casket.DeriveAgentKey([]byte(seed), slug))`-pub (recompute in the test) and the agent id is printed to stdout. A missing-`CW_OWNER_SEED` fail-fast test (no HTTP call).
- Gated live integration (`CW_IT_*` as **platform-admin**, skips offline): keygen a seed → provision a `cwb-test-*` org + a responsible human (as cwadmin, via `herald.CreateOrg`/`CreateHuman`) → `herald.CreateAgent` with a derived pubkey → then **log the agent in**: `assertion, _ := identity.AgentAssertion([]byte(seed), slug, agent.ID, tokenURL)`; `tok, _ := oidc.New(edge).JWTBearerGrant(ctx, assertion)`; `identity.DecodeAccessClaims(tok.AccessToken)` and assert `claims["scope"]` carries the granted scope and `claims["kind"]=="agent"`. Proves the full create→derive→register→login loop. (Reuse the `liveSession` helper pattern; the token URL comes from `oidc.New(edge).TokenEndpoint(ctx)` or the equivalent the login path uses — match the real method names.)

## Build order

Single cycle: `internal/herald.CreateAgent` → `internal/cli/agent` (keygen + create, register in main) → README `## Agents (herald admin)` + gated live test. Reuses the client seam + identity/oidc/casket unchanged.

## Future (deferred)

list/get/delete agents + scope mutation + key rotation (need new herald endpoints), `cw agent pubkey` (local derive/inspect helper), `cw whoami` (needs herald introspection). With #5 done, the CW CLI suite (#0–#5) is feature-complete for the agent + human + org + four-pillar provisioning loop.
