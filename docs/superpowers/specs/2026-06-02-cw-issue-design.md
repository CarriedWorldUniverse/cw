# `cw issue` — design

**Date:** 2026-06-02
**Status:** design (approved in brainstorming; pending written review)
**Sub-project:** #2 of the CW CLI suite — the ledger command group. Single cw-side cycle (ledger already exposes the full API; no ledger/proto change). Builds on the #0b `internal/client` seam + the #1b `internal/cmdutil` + `internal/<pillar>`-wrapper idiom.

## Goal

Give humans and agents the issue work-loop over ledger: create, list (mine/ready/by-project), view, claim, transition, comment — what the aspects use Jira for today — anchored on the same edge + herald token as the rest of `cw`.

## Platform grounding (what ledger exposes)

Through the gateway at `<edge>/ledger` (`/ledger` prefix stripped; herald bearer; scopes enforced). **Issue routes are org-scoped by the TOKEN, not the URL** — there is no `{org}` path segment (unlike cairn), so `cw issue` needs no repo-ref/org resolution. The caller's org comes from the bearer; `actor`/`reporter` are server-derived from `X-CWB-Subject` and are never sent by cw.

| cw command | ledger route | scope | response |
|---|---|---|---|
| create | `POST /api/issues` | `issue:write` | bare `Issue` (response_body:"issue") |
| view | `GET /api/issues/{key}` | `issue:read` | bare `Issue` |
| list --mine | `GET /api/issues/my` | `issue:read` | `{"issues":[IssueRef]}` |
| list --ready | `GET /api/issues/ready` | `issue:read` | `{"issues":[IssueRef]}` |
| list --project | `POST /api/issues/search` | `issue:read` | `{"refs":[IssueRef]}` |
| claim | `POST /api/issues/{key}/claim` | `issue:claim` | bare `Issue` |
| transition | `POST /api/issues/{key}/transition` | `issue:write` | (2xx; body not relied on) |
| comment | `POST /api/issues/{key}/comments` | `issue:write` | (2xx; empty body) |

Issues need a **working-org identity** with `issue:*` scopes; the genesis admin org is product-disabled (can't use ledger). ledger's gRPC edge marshals snake_case (`UseProtoNames`); cw decodes snake_case. `transition`/`comment` use grpc-gateway `body:"*"` — cw POSTs the non-path fields as JSON (key is in the path; `actor` server-derived).

JSON shapes (from `cwb.v1` ledger proto):
- `Issue{key, project, seq, type, status, summary, description, definition_of_done, priority, assignee_aspect, assignee_team, reporter, parent_key, external_refs, created_at, updated_at}`
- `IssueRef{key, project, type, status, summary, priority, assignee_aspect, assignee_team, updated_at}`

## Scope

**In:** `internal/ledger` wrapper + `cw issue create/list/view/claim/transition/comment`.

**Out (future):** assign, update (PATCH), links (add/list/rm), watchers (add/list/rm), text search (`/api/issues/search/text`), `/api/issues/updates`, projects create/list (`cw project` is its own future group — issues reference a project key the user supplies). No ledger/proto change in #2.

## Tech

Go 1.26; reuses `internal/client` (`Do([]byte)`/`Get`/`URL`/`ErrReauth`) + `internal/cmdutil` (`Session`). No org-resolution needed (token-org-scoped). No new deps.

---

## `internal/ledger` — API wrapper

Mirrors `internal/cairn`'s shape: a `do` helper (marshal → `c.Do(ctx, method, "ledger", path, raw)` → non-2xx error via shared message extraction → decode), typed funcs.

```go
package ledger

type Issue struct {
	Key              string `json:"key"`
	Project          string `json:"project"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Summary          string `json:"summary"`
	Description      string `json:"description"`
	DefinitionOfDone string `json:"definition_of_done"`
	Priority         string `json:"priority"`
	AssigneeAspect   string `json:"assignee_aspect"`
	Reporter         string `json:"reporter"`
	ParentKey        string `json:"parent_key"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}
type IssueRef struct {
	Key            string `json:"key"`
	Project        string `json:"project"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	Summary        string `json:"summary"`
	Priority       string `json:"priority"`
	AssigneeAspect string `json:"assignee_aspect"`
	UpdatedAt      string `json:"updated_at"`
}
type CreateInput struct {
	Project          string `json:"project"`
	Type             string `json:"type"`
	Summary          string `json:"summary"`
	Description      string `json:"description,omitempty"`
	DefinitionOfDone string `json:"definition_of_done,omitempty"`
	Priority         string `json:"priority,omitempty"`
}

func CreateIssue(ctx, c *client.Client, in CreateInput) (Issue, error)        // POST /api/issues -> bare Issue
func GetIssue(ctx, c *client.Client, key string) (Issue, error)               // GET /api/issues/{key}
func ListMine(ctx, c *client.Client) ([]IssueRef, error)                      // GET /api/issues/my  -> {"issues":[...]}
func ListReady(ctx, c *client.Client) ([]IssueRef, error)                     // GET /api/issues/ready
func SearchByProject(ctx, c *client.Client, project string) ([]IssueRef, error) // POST /api/issues/search {"filter":{"projects":[key]}} -> {"refs":[...]}
func Claim(ctx, c *client.Client, key string) (Issue, error)                  // POST .../claim -> bare Issue
func Transition(ctx, c *client.Client, key, status string) error             // POST .../transition {"status":...}; 2xx
func Comment(ctx, c *client.Client, key, body string) error                  // POST .../comments {"body":...}; 2xx
```

- `do`/`errMsg` are the same pattern as `internal/cairn` (surface ledger's `{"error"|"message"}` — e.g. the DoD-gate 400 on premature `Done`, scope 403). Path segments (`key`) are caller-typed → `url.PathEscape`. Content-Type application/json is set by the client seam.
- The two list wrappers decode `{"issues":[...]}`; search decodes `{"refs":[...]}`.
- `Transition`/`Comment` send only the non-path field (`status`/`body`); the key is in the path, `actor` is server-derived. They check 2xx and ignore the response body.

## `internal/cli/issue` — commands (on `cmdutil.Session`)

`NewCmd(*cmdutil.GlobalFlags)` → create/list/view/claim/transition/comment. No `--org`/`--repo` (ledger is token-org-scoped).

| Command | Behavior |
|---|---|
| `cw issue create` | `--project <KEY>` `--type <T>` `--title <s>` required (`--title`→`summary`); `--body`→description, `--dod`→definition_of_done, `--priority` optional. Fail-fast on missing required. Prints the new key to stdout, a confirmation to stderr. |
| `cw issue list` | `--mine` (default) / `--ready` / `--project <KEY>` (mutually-exclusive; `--project` → `SearchByProject`). Table `key  status  summary` to stdout, or `--json`. |
| `cw issue view <key>` | `GetIssue` → detail (key/type/status/summary/assignee/dod/…) or `--json`. |
| `cw issue claim <key>` | `Claim` → "claimed `<key>` (now `<status>`)" to stderr; key/status logic from the returned Issue. |
| `cw issue transition <key> <status>` | `Transition`; "`<key>` → `<status>`" to stderr. Surfaces the DoD-gate 400 verbatim. |
| `cw issue comment <key> <body>` | `Comment`; confirmation to stderr. |

Output convention: query (list/view) → stdout; mutations (create key, claim/transition/comment confirmations) → stderr; `--json` → stdout. Registered in `cmd/cw/main.go` after `pr`.

## Data flows

- **create:** `Session` → `ledger.CreateIssue(CreateInput{project,type,summary,…})` → print `issue.Key`.
- **list --project NEX:** `ledger.SearchByProject(c, "NEX")` → `POST /api/issues/search {"filter":{"projects":["NEX"]}}` → `[]IssueRef` → table.
- **transition NEX-12 "In Review":** `ledger.Transition(c, "NEX-12", "In Review")` → `POST /api/issues/NEX-12/transition {"status":"In Review"}` → 2xx (or DoD-gate 400 surfaced).

## Error handling

- non-2xx → ledger's error message (scope 403, DoD-gate 400 on premature Done, not-found 404, claim-conflict 409). `client.ErrReauth` → root's "session expired".
- `list` with more than one of `--mine`/`--ready`/`--project` → "specify one of --mine / --ready / --project".
- create missing a required flag → fail-fast usage error before any call.

## Testing

- `internal/ledger`: httptest-stub unit tests for each func (success + an error mapping incl. the transition DoD-gate 400 and claim 409); the wrapped (`issues`/`refs`) vs bare (`issue`) decodes; PathEscape on the key.
- `internal/cli/issue`: a command-level cobra-`Execute` wiring test (like #1b's `pr` test) — stub ledger, `--token` static client, run `cw issue list --mine` through cobra, assert the stub endpoint hit + output. Flag-validation tests (create required flags; list mutually-exclusive modes).
- Gated live integration (`CW_IT_*`, skips offline): as a working-org `issue:*` identity, create → list (--mine + --project) → view → claim → transition → comment against dMon. Same provisioning pattern as #1b (a `cwb-test-*` working org + a human with `issue:read/write/claim`).

## Build order

Single cycle: `internal/ledger` → `internal/cli/issue` → README + gated live test. Each reuses the client seam unchanged. (Per the #1b final-review hygiene, already in the shared code: JSON Content-Type on bodies, `url.PathEscape` on caller-supplied path segments — `internal/ledger` follows the same.)

## Future (deferred)

`cw issue assign/update`, links, watchers, text search, `/api/issues/updates` (a `--watch`/feed), and a `cw project` group (create/list projects). A `cw kb` (#3) and `cw org`/admin (#4) follow.
