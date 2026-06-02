# `cw` core + `cw auth` — design

**Date:** 2026-06-02
**Status:** design (approved in brainstorming; pending written review)
**Sub-project:** #0 of the CW CLI suite — the shared spine + authentication. All
later command groups (`repo`/`pr`, `issue`, `kb`, `org`/`admin`) import this and
are separate spec→plan→build cycles.

## Goal

A single `cw` binary that authenticates against the CWB platform and gives
humans **and** agents a working session, so every later command group is "add a
cobra subcommand that makes an authed call." This sub-project delivers the
shared core (config, contexts, token store, HTTP client, identity) and the
`cw auth` command group. It also delivers the one herald change auth depends on
(refresh tokens).

## The platform model `cw` is built on

Three statements from the operator fix the architecture:

1. **herald is AAA** — the one place you authenticate and are authorized.
2. **interchange is the gateway to the product** — the single edge that fronts
   the pillars (`/cairn`, `/ledger`, `/knowledge`) and herald (`/herald`).
3. **The only route through interchange without a valid bearer token is the
   route that gets you a bearer token** — i.e. the herald OIDC bootstrap
   (discovery + token + refresh + revoke). Everything else 401s without a valid
   bearer.

Therefore `cw` is anchored on **one URL: the interchange edge**. Under it:

| Route (under the edge) | Auth | Purpose |
|---|---|---|
| `/herald/.well-known/openid-configuration`, `/herald/jwks` | tokenless | OIDC discovery |
| `/herald/token`, `/herald/revoke` | tokenless (credential = grant input / refresh token) | get / refresh / revoke a bearer |
| `/cairn/*`, `/ledger/*`, `/knowledge/*`, herald's authed endpoints | **valid bearer required** | the product + authed surfaces |

`cw` derives the product route bases from the same edge (`<edge>/cairn`, …) — it
does **not** derive them from herald's URL. herald is simply the AAA the
tokenless auth-route speaks to. This matches what interchange already enforces
(conformance proves token-route-200 / gated-tokenless-401).

A **context** is therefore `{edge URL, identity}`; switching herald/edge switches
everything.

## Scope

**In scope (this sub-project):**

- herald **refresh tokens** (#0a) — issuance on both grants, the
  `refresh_token` grant, rotation, and revocation. Prerequisite for the human
  *and* agent token lifecycle in `cw`.
- `cw` **core** (#0b) — config + named contexts, token store (keychain refresh +
  cached access), the gateway HTTP client with silent refresh + edge routing,
  and identity sources (human password grant, agent casket assertion).
- `cw auth` command group — `login`, `logout`, `whoami`, `status`, `switch`,
  `token`.

**Out of scope (later cycles):** `cw repo`/`pr` (cairn), `cw issue` (ledger),
`cw kb` (commonplace), `cw org`/`admin` (herald). Also out: herald device-code /
auth-code / passkey login (the password grant ROPC is the human flow today).

## Tech choices

- **Go 1.26**, single static binary (matches the stack; reuses `casket-go`,
  `go-jose`, and the patterns in `cwb-conformance/internal/wire`).
- **cobra** command tree (gh/kubectl idiom).
- **`github.com/zalando/go-keyring`** for the OS keychain (macOS Keychain,
  Linux Secret Service) with an encrypted-file fallback.
- New **public repo** `github.com/CarriedWorldUniverse/cw`.

---

## Part A — herald refresh tokens (#0a, herald repo)

herald today issues only 10-minute access tokens (no refresh, nothing
persisted). `cw`'s "keychain + silent re-auth" lifecycle (for both humans and
agents, per the brainstorm) needs a refresh token.

### Store

New table `refresh_token`:

| column | notes |
|---|---|
| `id` | random 16-byte hex — the public handle (logged, not secret) |
| `token_hash` | SHA-256 of the opaque refresh token string (the secret is never stored plaintext, like `login_secret`) |
| `user_id` | FK → `user(id)` |
| `issued_at`, `expires_at` | refresh TTL, default 30d (`HERALD_REFRESH_TTL`) |
| `revoked_at` | nullable; set on logout / rotation supersede |
| `parent_id` | nullable; the rotated-from token id (replay detection) |

The opaque refresh token returned to the client is `"<id>.<secret>"` (id +
32-byte base64url secret); herald looks up by `id`, constant-time compares the
secret hash. Format keeps lookup O(1) without scanning hashes.

### Token endpoint changes (`internal/oidc`)

- **`HumanGrant` + `AgentGrant`**: on success, additionally mint + persist a
  refresh token and include `refresh_token` (+ existing `access_token`,
  `token_type`, `expires_in`) in the response.
- **New `RefreshGrant`** wired into the existing `GrantMux`
  (`grant_type=refresh_token`, param `refresh_token`): validate (lookup by id,
  hash match, not expired, not revoked) → mint a fresh access token → **rotate**
  (revoke the presented refresh token, issue a new one linked via `parent_id`,
  return it). Reuse of an already-rotated (revoked) refresh token →
  `invalid_grant` **and** revoke the whole chain (replay defense).
- The new access token carries the same `sub/kind/org/scope/products` claims the
  original grant would (re-derived from the user at refresh time, so scope/
  product changes take effect on refresh).

### Revocation (RFC 7009-style)

`POST /revoke` (param `token` = a refresh token) → mark revoked (+ its chain).
Idempotent, always 200 (no token enumeration). This is the `cw auth logout`
path. It is a tokenless auth-route (the refresh token authenticates the call).

### Config / behavior

- `HERALD_REFRESH_TTL` (default `720h` = 30d); rotation always on.
- Access-token TTL unchanged (10m).
- Tokenless routes through interchange extend to `/herald/revoke` (already covers
  `/herald/token` + discovery) — confirm the interchange herald composite passes
  `revoke` through unauthenticated (it sits beside `token`).

### Tests

- Unit (`internal/oidc`): issue→refresh→new-access; rotation revokes old;
  reuse-revoked→invalid_grant + chain revoked; expiry; revoke→subsequent
  refresh fails.
- Conformance (herald layer): extend with issue (login) → refresh → use new
  access on a gated call (200) → revoke → refresh-after-revoke (fail). Drives the
  flow `cw` will use, through the gateway.

---

## Part B — `cw` core + `cw auth` (#0b, cw repo)

### Package layout

```
cw/
  cmd/cw/main.go               # cobra root, global flags, command registration
  internal/
    config/                    # ~/.config/cw/config.yaml: contexts, current-context
    tokenstore/                # keychain (refresh) + cached access token; fake for tests
    oidc/                      # discovery fetch + token/refresh/revoke grant calls
    identity/                  # human prompt; agent keyfile/env -> casket derive + assert
    client/                    # edge-anchored HTTP client: bearer inject, silent refresh, route helpers
    cli/auth/                  # login, logout, whoami, status, switch, token subcommands
```

`internal/client` is the seam every later command group builds on: give it a
context, it returns an authed `*http.Client`-like that targets `<edge>/<pillar>`
and transparently keeps the access token fresh.

### Config — `~/.config/cw/config.yaml` (0600)

```yaml
current-context: dev
contexts:
  dev:
    edge: http://dmonextreme.tail41686e.ts.net:8080
    identity:
      kind: human            # human | agent
      subject: ""            # herald user id, filled from whoami after login
      display: ""            # email / display name, for UX
  prod:
    edge: https://cwb.carriedworld.com
    identity:
      kind: agent
      subject: <agent-id>
      slug: shadow
```

- **Non-secret only.** No tokens, no passwords, no seeds in this file.
- Access token cache: `~/.config/cw/tokens/<context>.json` (0600) — ephemeral.
- Refresh token: **OS keychain**, service `cw`, key `<edge>|<subject>`.
- Agent owner-seed: **never** in config — from `CW_OWNER_SEED` env or a keychain
  entry `cw-seed|<edge>|<subject>`. The agent identity file (below) holds only
  non-secret `{edge, agent-id, slug}`.

### Global flags / env (precedence: flag > env > current context)

- `--context <name>` (`CW_CONTEXT`) — pick a stored context.
- `--edge <url>` (`CW_EDGE`) — override the edge (ad-hoc / first login).
- `--token <jwt>` (`CW_TOKEN`) — present a bearer directly; **skips the store/
  refresh path entirely** (the stateless per-invocation path ToolRunner agents
  use). With `--token`, `cw` makes the product call and never touches keychain.
- `--identity <path>` (`CW_IDENTITY`) — agent identity file for `--agent` login.
- `--json` — machine output; default is human tables.

### Identity sources

- **Human:** `cw auth login` prompts email + password → password grant. Username
  resolves by id / email / display name (herald login-by-email, Phase 5a).
- **Agent:** `cw auth login --agent` loads `{edge, agent-id, slug}` from the
  identity file (or `--agent-id/--slug` flags / `CW_AGENT_*` env) + the owner
  seed from `CW_OWNER_SEED`/keychain → `casket.DeriveAgentKey(seed, slug)` →
  signs a jwt-bearer assertion (`iss=sub=agent-id`, `aud=<token endpoint>`) →
  jwt-bearer grant. (by-fingerprint can't resolve the id externally — it is
  mTLS-internal — so the agent-id is supplied, not discovered.)

### Token lifecycle (both kinds)

1. `login` → grant → `{access(10m), refresh(30d)}`. Refresh → keychain; access →
   cache file; `subject/display` written back to the context (via a `whoami`
   decode of the access token claims).
2. Any authed call: if cached access token is within a 60s skew of expiry → run
   the **`refresh_token` grant** silently → update cache + rotated refresh in
   keychain → proceed.
3. Refresh fails (expired/revoked refresh):
   - **agent:** re-mint from the seed (jwt-bearer) if available → new
     access+refresh; else error.
   - **human:** error `run 'cw auth login'` (interactive: offer to prompt).
4. `logout` → `POST /herald/revoke` the refresh token → wipe keychain + cache;
   leave the context entry (so `switch` back just needs a re-login).

### `cw auth` commands

| Command | Behavior |
|---|---|
| `cw auth login` | human: prompt + password grant. `--agent`: assertion + jwt-bearer grant. `--edge`/`--context` select the target; first login may create the context. Stores tokens, writes back identity. |
| `cw auth logout` | revoke refresh token, wipe keychain + access cache for the context. |
| `cw auth whoami` | decode the (refreshed) access token → print `subject, kind, org, scopes, products, expires-in`. `--json` for raw claims. |
| `cw auth status` | list contexts, mark current, show each identity + token freshness (valid / refreshable / logged-out). |
| `cw auth switch <name>` | set `current-context`. |
| `cw auth token` | print a currently-valid access token to stdout (auto-refreshing first) — for piping into `curl`/scripts. |

### Data flows

- **Human login:** discovery(`<edge>/herald`) → `POST <token_endpoint>`
  `grant_type=password` → store → `whoami` writes subject/display.
- **Agent login:** load identity+seed → derive key → sign assertion → `POST`
  `grant_type=jwt-bearer` → store.
- **Authed product call:** resolve context → ensure-fresh (refresh if stale) →
  `GET <edge>/<pillar>/…` with `Authorization: Bearer`. On a 401 despite a
  fresh-looking token, refresh once and retry; still 401 → surface.
- **Logout:** `POST <edge>/herald/revoke` token=refresh → wipe.

### Error handling

- Discovery unreachable → `cannot reach herald at <edge> (…)`.
- `invalid_grant` on login → `invalid credentials` (no enumeration, mirrors
  herald's uniform 401).
- Keychain unavailable → encrypted-file fallback in `~/.config/cw/` (0600) with a
  one-line stderr warning.
- 403 from a product route → surface herald's message verbatim (scope /
  product-entitlement denial).
- `--token` present but expired → surface the 401 (no refresh path; the caller
  owns the token).

### Tests

- Unit: config load/save + context precedence; tokenstore against a fake
  keyring; oidc discovery/grant parsing; identity agent-assertion (casket round
  trip vs a known pubkey); `client` silent-refresh + 401-retry against an
  `httptest` herald+edge double.
- Integration (gated, needs herald #0a deployed): against dMon — human login as
  the genesis owner (`cwadmin`, password from the cluster secret) → `whoami` →
  force-expire → silent refresh → `auth token` works → `logout` → next call
  fails. An agent variant using a fixture casket key.

---

## Build order

1. **#0a herald refresh tokens** — land + deploy first (cw's refresh path can be
   unit-tested against a double meanwhile, but integration needs it live).
2. **#0b cw core + `cw auth`** — build on the live refresh grant.

Each becomes its own implementation plan (writing-plans). #0b's `internal/client`
is the contract the later command groups (`repo`/`pr`, `issue`, `kb`,
`org`/`admin`) consume — those are subsequent sub-projects.

## Future (explicitly deferred)

- herald device-code / auth-code + login page / passkey (richer human login).
- `cw` shell completion, `cw config` editing UX, multiple identities per context.
- Token-bound product caching, offline mode.
