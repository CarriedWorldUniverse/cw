# `cw repo` / `cw pr` — design

**Date:** 2026-06-02
**Status:** design (approved in brainstorming; pending written review)
**Sub-project:** #1 of the CW CLI suite — the cairn command group (the "gh equivalent"). Builds on the #0b `internal/client` seam. Two parts: **1a** extends cairn with list endpoints; **1b** is the cw command group.

## Goal

Give humans and agents a CLI over cairn: create/list/clone repos and create/list/view/merge pull requests, anchored on the same edge + herald token as `cw auth`. A faithful `gh`-style workflow against what cairn supports.

## Platform grounding (what cairn exposes)

Through the gateway at `<edge>/cairn` (the `/cairn` prefix is stripped before reaching cairn; auth is the herald bearer, scopes enforced):

| Operation | Route | Scope | Status |
|---|---|---|---|
| Create repo | `POST /api/orgs/{org}/repos` | `repo:write` | exists |
| Open PR | `POST /api/orgs/{org}/repos/{slug}/pulls` | `repo:write` | exists |
| View PR | `GET /api/orgs/{org}/repos/{slug}/pulls/{id}` | `repo:read` | exists |
| Merge PR | `POST /api/orgs/{org}/repos/{slug}/pulls/{id}/merge` | `repo:write` | exists (ff-only) |
| **List repos** | `GET /api/orgs/{org}/repos` | `repo:read` | **add (1a)** |
| **List PRs** | `GET /api/orgs/{org}/repos/{slug}/pulls?state=` | `repo:read` | **add (1a)** |
| git clone/push | `<edge>/cairn/{org}/{slug}.git` (Smart-HTTP) | bearer | exists |

`{org}` in these paths is the herald **org id** (the UUID in the token's `org` claim), not a name — cairn's repo API has no org-name namespace. `OpenPull` requires `source`, `target`, `title`, `project` (a ledger project key); `description` + `definition_of_done` are optional. cairn's repo core already has `ListRepos(orgID)`; pulls already carry `state` (`open`/`merged`).

The cairn gRPC edge marshals JSON snake_case (`UseProtoNames` + `EmitUnpopulated`); cw decodes snake_case.

## Scope

**In:**
- **1a (cairn + cwb-proto + conformance):** `RepoService.ListRepos` (core method exists — wire the RPC) and `PullService.ListPulls` (add a `ListPulls(repoID, state)` core method + the RPC), with `google.api.http` annotations and a conformance assertion.
- **1b (cw):** an `internal/cairn` API-wrapper package + `cw repo create/list/clone` and `cw pr create/list/view/merge` commands on the `internal/client` seam.

**Out (future / other cycles):** repo delete (org-purge only today), PR close/reopen/comment, PR reverse-sync from ledger, pagination (orgs are small — list returns all), the cairn web UI, human-readable org names (needs herald name→id resolution — `cw org` territory), branch/diff browsing.

## Tech

Go 1.26; cw reuses `internal/client` (`Do([]byte)`/`Get`/`URL`/`AccessToken`/`ErrReauth`) + `internal/config`; clone/push shell out to the system **`git`** (must be on PATH). cairn side: existing buf/grpc-gateway + `repo.Service` + sqlite.

---

## Part 1a — cairn list endpoints

### cwb-proto (`cairn.v1`)

Add two RPCs to the existing services, mirroring the existing annotation style:

```proto
// in RepoService
rpc ListRepos(ListReposRequest) returns (ListReposResponse) {
  option (google.api.http) = {get: "/api/orgs/{org}/repos"};
}
// in PullService
rpc ListPulls(ListPullsRequest) returns (ListPullsResponse) {
  option (google.api.http) = {get: "/api/orgs/{org}/repos/{slug}/pulls"};
}

message ListReposRequest { string org = 1; }                 // path
message ListReposResponse { repeated Repo repos = 1; }        // response_body: "repos"
message ListPullsRequest { string org = 1; string slug = 2; string state = 3; } // state via ?state=
message ListPullsResponse { repeated Pull pulls = 1; }        // response_body: "pulls"
```

Reuse the existing `Repo` and `Pull` messages. Set `response_body` so the REST shape is a bare JSON array (matching how the other cairn list-ish responses are flattened). Regenerate gen-Go; the CI buf checks already pass for additive changes.

### cairn repo core

`ListRepos(ctx, orgID)` already exists. Add:

```go
// ListPulls returns the repo's pull requests, newest first. state "" or "all"
// returns every state; otherwise filters by exact state ("open"|"merged").
func (s *Service) ListPulls(ctx context.Context, repoID, state string) ([]Pull, error)
```

Query `pull_request WHERE repo_id = ? [AND state = ?] ORDER BY created_at DESC`. Unit-test (open + merged + all).

### cairn grpcapi

- `repoServer.ListRepos`: `authed(ctx, req.Org, "repo:read")` → `core.ListRepos(org)` → map to proto. (Lists the caller-authorized org's repos.)
- `pullServer.ListPulls`: `authed(ctx, req.Org, "repo:read")` → `GetRepo(org, slug)` (404 if absent) → `core.ListPulls(repo.ID, req.State)` → map via `toProtoPull`.
Bufconn unit tests for both, mirroring the existing `grpcapi_test.go` patterns.

### conformance

Extend the cairn layer: after the existing SSH round-trip + a PR open, assert `ListRepos` includes the fixture repo and `ListPulls` (state=open) includes the opened PR. Reuse the fixture org/token.

### deploy

Build + import cairn image on dMon, rollout, `cwb-conform -target dmon -layers cairn,all` green incl. the new list assertions. (No interchange change — `/cairn/api/*` already routes to gRPC.)

---

## Part 1b — cw `repo` + `pr` command group

### `internal/cairn` (API wrapper)

A thin package mapping cairn's REST surface to typed Go, all via a `*client.Client`. Keeps `cli/` thin (same layering as the auth commands consume `client` directly, but cairn's surface is big enough to warrant its own wrapper).

```go
package cairn

type Repo struct {
	Slug          string `json:"slug"`
	Org           string `json:"org"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"` // if cairn returns one; else cw builds it
}
type Pull struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Title     string `json:"title"`
	State     string `json:"state"`
	IssueKey  string `json:"issue_key"`  // the ledger issue cairn created
	MergedSHA string `json:"merged_sha"`
}

func CreateRepo(ctx, c *client.Client, org, slug string) (Repo, error)
func ListRepos(ctx, c *client.Client, org string) ([]Repo, error)
func OpenPull(ctx, c *client.Client, org, slug string, in OpenPullInput) (Pull, error)
func ListPulls(ctx, c *client.Client, org, slug, state string) ([]Pull, error)
func GetPull(ctx, c *client.Client, org, slug, id string) (Pull, error)
func MergePull(ctx, c *client.Client, org, slug, id string) (Pull, error)
```

Each builds the path under the `cairn` pillar (`c.URL("cairn", "/api/orgs/"+org+"/repos"+…)`), calls `c.Do`/`c.Get`, checks the status, decodes snake_case JSON, and maps non-2xx to a clear error (surfacing cairn's message — e.g. the merge 409 "not fast-forward"). Field names are confirmed against cairn's `toProtoRepo`/`toProtoPull` during implementation; the structs above are decoded leniently (unknown fields ignored).

### Org resolution

The default org is the caller's org id from the access token's `org` claim (decode via `identity.DecodeAccessClaims`, as `whoami` does). A repo ref may be `<slug>` (caller's org) or `<org-id>/<slug>` (explicit). A `--org <id>` flag overrides. (Human-readable org names are future — herald name→id resolution.)

### Commands (`internal/cli/repo`, `internal/cli/pr`; registered on the root)

| Command | Maps to | Notes |
|---|---|---|
| `cw repo create <slug>` | CreateRepo | prints slug + clone URL (stderr confirmation) |
| `cw repo list` | ListRepos | table (`slug  default-branch`) / `--json` |
| `cw repo clone <ref> [dir]` | git | builds `<edge>/cairn/<org>/<slug>.git`, shells `git -c http.extraHeader=Authorization: Bearer <fresh token> clone <url> [dir]`; the token is fetched fresh via `client.AccessToken` |
| `cw pr create` | OpenPull | flags `--head <source>` `--base <target>` `--title` `--project <KEY>` `[--body]` `[--dod]`; repo from `--repo <ref>` or cwd inference (see below); prints PR id + ledger issue key |
| `cw pr list` | ListPulls | `--repo <ref>`; `--state open\|merged\|all` (default `open`); table / `--json` |
| `cw pr view <id>` | GetPull | `--repo <ref>`; detail / `--json` |
| `cw pr merge <id>` | MergePull | `--repo <ref>`; ff-only; surfaces cairn's 409 with a clear "not a fast-forward" message + the merged SHA on success |

**Repo inference for `pr` commands:** `--repo <ref>` is explicit. If omitted and the cwd is a git work tree whose `origin` remote URL matches `<edge>/cairn/<org>/<slug>.git`, infer `<org>/<slug>` from it (parse the remote). Otherwise require `--repo`. (Inference keeps `cw pr create` ergonomic inside a clone; the parse is a small helper.)

### Transport

- **API calls** (`repo create/list`, all `pr` subcommands) go through `internal/client` (bearer + silent refresh).
- **clone** shells out to `git`, injecting the bearer for that one invocation via `-c http.extraHeader=Authorization: Bearer <fresh token>`. Nothing persistent is embedded in the clone (the token is short-lived; baking it into the remote would stale out).
- **push is NOT a cw command in v1.** After cloning, the user pushes with their own `git` — supplying a token via `git -c http.extraHeader=...` (or `cw auth token` piped in) per push. A `cw` git credential-helper that feeds fresh bearers to `git` automatically is deferred (it's the clean long-term answer but its own piece of work). v1 documents the `git -c http.extraHeader="Authorization: Bearer $(cw auth token)" push` recipe in the README.

---

## Data flows

- **`cw repo list`:** session → `client.Get("cairn", "/api/orgs/<org>/repos")` → decode `[]Repo` → table/json.
- **`cw pr create`:** resolve repo ref + org → `client.Do(POST, "cairn", "/api/orgs/<org>/repos/<slug>/pulls", json{source,target,title,project,description,definition_of_done})` → decode `Pull` → print id + `issue_key`.
- **`cw repo clone`:** `client.AccessToken(ctx)` (fresh) → `exec git -c http.extraHeader="Authorization: Bearer <tok>" clone <edge>/cairn/<org>/<slug>.git [dir]` → stream git's stdout/stderr.
- **`cw pr merge`:** `client.Do(POST, …/pulls/<id>/merge)` → 2xx decode `Pull` (merged_sha); 409 → "not a fast-forward".

## Error handling

- API non-2xx → surface cairn's error body/message (e.g. scope `repo:write` denial 403, repo-not-found 404, merge 409). `ErrReauth` from the client → the root's `cw: session expired` message.
- `clone`: a non-zero `git` exit → return the git stderr verbatim; missing `git` on PATH → clear "git not found on PATH" error.
- Ambiguous/absent repo ref on `pr` commands with no inferable cwd remote → "specify --repo <org>/<slug>".

## Testing

- **1a cairn:** core `ListPulls` unit test (open/merged/all); grpcapi bufconn tests for `ListRepos`/`ListPulls` (incl. scope `repo:read`, repo-not-found 404); conformance cairn-layer list assertions; deploy + `-layers cairn,all` green on dMon.
- **1b cw:** `internal/cairn` unit tests against an httptest stub (each call: success + a non-2xx error mapping incl. merge 409); a repo-ref/remote-URL parser unit test; cli command tests (flag wiring, json output) using a stub; clone tested via the gated live smoke (needs a real git server). Extend the gated `TestLiveLogin`-style integration with a repo create→clone→pr create→list→merge loop against dMon (skip without `CW_IT_*`).

## Build order

1. **1a cairn list endpoints** — proto + core + grpcapi + conformance; deploy + verify green on dMon (so cw's `list` has live endpoints).
2. **1b cw repo/pr** — `internal/cairn` + the commands; unit-tested offline, live-verified against dMon.

Each is its own implementation plan (writing-plans). 1b consumes the `internal/client` seam unchanged.

## Future (explicitly deferred)

`cw repo delete`, `cw pr close/comment`, `cw repo push` + a `cw` git credential helper, pagination, human-readable org names (`cw org` + herald resolution), branch/diff/file browsing, the cairn web UI.
