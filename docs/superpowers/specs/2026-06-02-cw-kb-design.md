# `cw kb` — design

**Date:** 2026-06-02
**Status:** design (approved in brainstorming)
**Sub-project:** #3 of the CW CLI suite — the commonplace (knowledge) command group. Single cw-side cycle (commonplace already exposes the needed API). Builds on the #0b `internal/client` seam + the #1b/#2 `internal/cmdutil` + `internal/<pillar>`-wrapper idiom.

## Goal

Give humans and agents the knowledge store/recall loop over commonplace: store an entry, semantic search (the headline — embedding retrieval, not keyword), and list — anchored on the same edge + herald token as the rest of `cw`.

## Platform grounding (what commonplace exposes)

Through `<edge>/knowledge` (`/knowledge` prefix stripped; herald bearer; scopes enforced). **Knowledge routes are token-org-scoped — no `{org}` path segment** (like ledger), so `cw kb` needs no org-resolution. `owner` is server-derived from `X-CWB-Subject`; never sent.

| cw command | route | scope | response |
|---|---|---|---|
| store | `POST /api/knowledge` | `knowledge:write` | bare `Entry` (response_body:"entry") |
| search | `GET /api/knowledge/search?q=&top_k=` | `knowledge:read` | `{"hits":[{entry,score}]}` |
| list | `GET /api/knowledge` | `knowledge:read` | `{"entries":[Entry]}` |

No REST get-by-id: commonplace's `Get` is gRPC-only (its HTTP binding was dropped in Phase 0/1 because the `{id}` wildcard shadowed `/search`). It isn't needed — `Search`/`List` return full `Entry` objects. commonplace embeds via ollama on dMon, so a live store→semantic-search round-trip is a real check. Needs a working-org `knowledge:*` identity (the admin org is product-disabled). snake_case JSON.

JSON shapes (`cwb.v1` commonplace proto):
- `Entry{id, org, owner, topic, content, visibility, tags[], created_at, updated_at}` (`visibility` = "private" | "org")
- `Hit{entry: Entry, score: double}`
- `StoreRequest{topic, content, visibility, tags[]}`

## Scope

**In:** `internal/commonplace` wrapper + `cw kb store/search/list`.

**Out (future):** `cw kb update` (PATCH /{id}) + `cw kb delete` (DELETE /{id}) — both exist server-side, deferred; get-by-id (no REST binding; search/list cover retrieval); tag-filtered list (List takes no filter today). No commonplace/proto change in #3.

## Tech

Go 1.26; reuses `internal/client` (`Do([]byte)`/`Get`/`URL`/`ErrReauth`) + `internal/cmdutil` (`Session`). No org-resolution. No new deps.

---

## `internal/commonplace` — API wrapper

Mirrors `internal/cairn`/`internal/ledger`: a `do` helper (marshal → `c.Do(ctx, method, "knowledge", path, raw)` → non-2xx error via shared message extraction → decode) + typed funcs.

```go
package commonplace

type Entry struct {
	ID         string   `json:"id"`
	Org        string   `json:"org"`
	Owner      string   `json:"owner"`
	Topic      string   `json:"topic"`
	Content    string   `json:"content"`
	Visibility string   `json:"visibility"`
	Tags       []string `json:"tags"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}
type Hit struct {
	Entry Entry   `json:"entry"`
	Score float64 `json:"score"`
}
type StoreInput struct {
	Topic      string   `json:"topic"`
	Content    string   `json:"content"`
	Visibility string   `json:"visibility,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

func Store(ctx, c *client.Client, in StoreInput) (Entry, error)            // POST /api/knowledge -> bare Entry
func Search(ctx, c *client.Client, q string, topK int) ([]Hit, error)     // GET /api/knowledge/search?q=&top_k= -> {"hits":[...]}
func List(ctx, c *client.Client) ([]Entry, error)                         // GET /api/knowledge -> {"entries":[...]}
```

- `do`/`errMsg` are the same pattern as `internal/cairn` (surface commonplace's `{"error"|"message"}` — e.g. scope 403). Pillar = `"knowledge"`. Content-Type application/json set by the client seam.
- `Search` builds the query string with `url.Values{"q":{q},"top_k":{strconv.Itoa(topK)}}.Encode()` (proper escaping for the free-text `q`); decodes `{"hits":[Hit]}`.
- `List` decodes `{"entries":[Entry]}`.
- The pillar prefix on the gateway is `/knowledge` (per `CWB_KNOWLEDGE_PATH`/the gateway route map), so `c.URL("knowledge", "/api/knowledge")` = `<edge>/knowledge/api/knowledge`.

## `internal/cli/kb` — commands (on `cmdutil.Session`)

`NewCmd(*cmdutil.GlobalFlags)` → store/search/list. No `--org`.

| Command | Behavior |
|---|---|
| `cw kb store --topic <t>` | content from `--content` else **stdin** (read all; error if both empty/no stdin). `--visibility` default `org` (`private` opt); `--tag` repeatable (`StringArrayVar`). Prints the new entry id to stdout, a confirmation to stderr. |
| `cw kb search <query>` | `--top-k` default 5. Prints `score  topic` + a one-line content snippet per hit to stdout, or `--json` (the full `[]Hit`). |
| `cw kb list` | Prints `id  visibility  topic` per entry to stdout, or `--json` (`[]Entry`). |

Output convention: query (search/list) → stdout; store (id → stdout, confirmation → stderr); `--json` → stdout. Registered in `cmd/cw/main.go` after `issue`.

`store` content sourcing: if `--content` is set use it; else read all of stdin (`io.ReadAll(os.Stdin)`); if both yield empty → error "provide content via --content or stdin". This makes `cw kb store --topic x < doc.md` and `echo ... | cw kb store --topic x` both work.

## Data flows

- **store:** `Session` → read content (flag/stdin) → `commonplace.Store(StoreInput{topic, content, visibility, tags})` → print `entry.ID`.
- **search "how does auth work":** `commonplace.Search(c, q, 5)` → `GET /knowledge/api/knowledge/search?q=how+does+auth+work&top_k=5` → `[]Hit` → `score topic\n  snippet` lines.
- **list:** `commonplace.List(c)` → `[]Entry` → table.

## Error handling

- non-2xx → commonplace's error message (scope 403, validation 400). `client.ErrReauth` → root's "session expired".
- store with no content (flag empty + empty stdin) → fail-fast "provide content via --content or stdin".
- `--topic` required → fail-fast before the call.

## Testing

- `internal/commonplace`: httptest-stub unit tests for Store/Search/List (success + a 403 scope error mapping); bare `Entry` vs wrapped `{hits}`/`{entries}` decodes; the search query string carries `q` + `top_k`.
- `internal/cli/kb`: a command-level cobra-`Execute` wiring test (stub commonplace, `--token` static, run `cw kb list` through cobra, assert the endpoint hit + output); a `store` stdin-vs-`--content` sourcing test (content read from a piped reader).
- Gated live integration (`CW_IT_*`, skips offline): as a working-org `knowledge:*` identity, store an entry → search with a DIFFERENTLY-worded query that surfaces it (proving live semantic retrieval via ollama) → list. Same provisioning pattern as #1b/#2.

## Build order

Single cycle: `internal/commonplace` → `internal/cli/kb` → README + gated live test. Reuses the client seam unchanged; inherits the shared hygiene (JSON Content-Type, query/path escaping).

## Future (deferred)

`cw kb update/delete`, tag-filtered list, get-by-id (needs a non-shadowing REST binding on commonplace), then **#4 `cw org`/admin** (herald gRPC admin via the gateway) — the last command group.
