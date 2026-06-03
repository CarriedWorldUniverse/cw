# `cw kb update` / `cw kb delete` Implementation Plan (#10)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Two-repo cycle: add `Update`/`Delete` to `cwb-client/commonplace` (Task 1), then cw bumps + adds the commands (Tasks 2–3). Cross-repo merge/pin is a CONTROLLER step between Task 1 and Task 2.

**Goal:** Complete the `cw kb` CRUD — `cw kb update`/`delete` over commonplace's already-live `Update`/`Delete`.

**Architecture:** `cwb-client/commonplace` gains the two wrappers; `cw/internal/cli/kb` gains the two commands. Owner-scoped + `knowledge:write` (server-enforced; cw surfaces). No proto/interchange/deploy.

**Tech:** Go 1.26. Spec: `cw/docs/superpowers/specs/2026-06-03-cw-kb-update-delete-design.md`.

## Verified facts

- `cwb-client/commonplace/commonplace.go`: `do(ctx,c,method,path,body,out) error` (pillar `knowledge`, `base = "/api/knowledge"`, imports `net/http`+`net/url`), `errMsg`, `Store`/`Search`/`List`, types `Entry`/`Hit`/`StoreInput`. `do` with `out=nil` is a 2xx-only check (204 passes). `client.Do` takes the method string → PATCH/DELETE work.
- commonplace: `Update` = `PATCH /api/knowledge/{id}` `body:"*"` `response_body:"entry"` → bare `Entry`, **partial** (empty/absent fields unchanged). `Delete` = `DELETE /api/knowledge/{id}` → 204, hard-delete. Both `knowledge:write` + owner-scoped.
- `cw/internal/cli/kb/kb.go`: `NewCmd` adds `newStoreCmd/newSearchCmd/newListCmd`; imports `cwb-client/commonplace` + `cmdutil` + cobra. cw pins `cwb-client` (bump it in Task 2).

---

## Task 1: `cwb-client/commonplace` — `Update` + `Delete`

**Repo:** `/Users/jacinta/Source/cwb-client` (branch `nex-kb-crud`)
**Files:** `commonplace/commonplace.go`, `commonplace/commonplace_test.go`

- [ ] **Step 1: Write the failing test** — append to `commonplace/commonplace_test.go` (add `io` to its imports if absent; it already has `context`/`net/http`/`httptest`/`strings`/`testing` + `client`):

```go
func TestUpdateDelete(t *testing.T) {
	var patchBody string
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /knowledge/api/knowledge/e1", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		patchBody = string(b)
		_, _ = w.Write([]byte(`{"id":"e1","topic":"new","content":"c","visibility":"org"}`))
	})
	mux.HandleFunc("DELETE /knowledge/api/knowledge/e1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.WithStaticToken(srv.URL, "tok")
	ctx := context.Background()

	topic := "new"
	e, err := Update(ctx, c, "e1", UpdateInput{Topic: &topic})
	if err != nil || e.ID != "e1" || e.Topic != "new" {
		t.Fatalf("Update: %v %+v", err, e)
	}
	// only the changed field is sent.
	if !strings.Contains(patchBody, `"topic":"new"`) || strings.Contains(patchBody, "content") {
		t.Fatalf("patch body should carry only topic: %s", patchBody)
	}
	if err := Delete(ctx, c, "e1"); err != nil { // 204
		t.Fatalf("Delete: %v", err)
	}
}

func TestUpdateError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /knowledge/api/knowledge/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.WithStaticToken(srv.URL, "tok")
	topic := "x"
	if _, err := Update(context.Background(), c, "missing", UpdateInput{Topic: &topic}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("Update error: want server message, got %v", err)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** — `cd /Users/jacinta/Source/cwb-client && go test ./commonplace/ -run 'TestUpdate|TestUpdateDelete'`
Expected: build error (undefined `UpdateInput`/`Update`/`Delete`).

- [ ] **Step 3: Implement** — add to `commonplace/commonplace.go`:

```go
// UpdateInput patches a knowledge entry. Only non-nil fields are sent; the
// server leaves unsupplied fields unchanged. Tags, when set, fully replaces.
type UpdateInput struct {
	Topic      *string   `json:"topic,omitempty"`
	Content    *string   `json:"content,omitempty"`
	Visibility *string   `json:"visibility,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
}

// Update patches an entry by id (PATCH /api/knowledge/{id}) -> the updated Entry.
func Update(ctx context.Context, c *client.Client, id string, in UpdateInput) (Entry, error) {
	var e Entry
	err := do(ctx, c, http.MethodPatch, base+"/"+url.PathEscape(id), in, &e)
	return e, err
}

// Delete removes an entry by id (DELETE /api/knowledge/{id}) -> 2xx-only (204).
func Delete(ctx context.Context, c *client.Client, id string) error {
	return do(ctx, c, http.MethodDelete, base+"/"+url.PathEscape(id), nil, nil)
}
```

- [ ] **Step 4: Run — expect PASS** — `cd /Users/jacinta/Source/cwb-client && go test ./commonplace/ -v && go build ./... && go vet ./commonplace/`

- [ ] **Step 5: Commit** — `git add commonplace/ && git commit -m "commonplace: Update + Delete (PATCH/DELETE /api/knowledge/{id})"` (trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`). Do NOT push.

> **CONTROLLER (between Task 1 and Task 2):** push `nex-kb-crud`, merge to `cwb-client` main (squash), capture the merged-main short hash `H2`. Provide `H2` to Task 2.

---

## Task 2: `cw kb update` / `cw kb delete` commands

**Repo:** `/Users/jacinta/Source/cw` (branch `nex-cw-kb-crud`)
**Files:** `go.mod` (bump), `internal/cli/kb/kb.go`, `internal/cli/kb/kb_test.go`

- [ ] **Step 1: Bump the lib** — `cd /Users/jacinta/Source/cw && go get github.com/CarriedWorldUniverse/cwb-client@<H2> && go mod tidy`

- [ ] **Step 2: Write the failing command tests** — append to `internal/cli/kb/kb_test.go`:

```go
func TestKbUpdateWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var body, path string
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /knowledge/api/knowledge/e1", func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"id":"e1","topic":"new","visibility":"org"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"update", "e1", "--topic", "new"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if path != "/knowledge/api/knowledge/e1" || !strings.Contains(body, `"topic":"new"`) || strings.Contains(body, "content") {
		t.Fatalf("path=%q body=%q", path, body)
	}
}

func TestKbUpdateNothing(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /knowledge/api/knowledge/e1", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"update", "e1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected nothing-to-update error")
	}
	if called {
		t.Fatal("update with no flags must not hit the server")
	}
}

func TestKbDeleteWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	hit := false
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /knowledge/api/knowledge/e1", func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"delete", "e1", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !hit {
		t.Fatal("delete endpoint not hit")
	}
}

func TestKbDeleteRequiresYes(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /knowledge/api/knowledge/e1", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"delete", "e1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected --yes-required error")
	}
	if called {
		t.Fatal("delete without --yes must not hit the server")
	}
}
```

(Ensure the test file imports `io`/`net/http`/`net/http/httptest`/`strings` — it already has the kb wiring-test pattern; add any missing.)

- [ ] **Step 3: Run — expect FAIL** — `cd /Users/jacinta/Source/cw && go test ./internal/cli/kb/ -run 'TestKbUpdate|TestKbDelete'` (unknown command).

- [ ] **Step 4: Implement** in `internal/cli/kb/kb.go` — register both in `NewCmd`:
```go
	cmd.AddCommand(newStoreCmd(gf), newSearchCmd(gf), newListCmd(gf), newUpdateCmd(gf), newDeleteCmd(gf))
```
and add:
```go
func newUpdateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var topic, content, visibility string
	var tags []string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a knowledge entry (only the flags you set; --tag replaces all tags)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			var in commonplace.UpdateInput
			if f.Changed("topic") {
				in.Topic = &topic
			}
			if f.Changed("content") {
				in.Content = &content
			}
			if f.Changed("visibility") {
				in.Visibility = &visibility
			}
			if f.Changed("tag") {
				in.Tags = &tags
			}
			if in.Topic == nil && in.Content == nil && in.Visibility == nil && in.Tags == nil {
				return fmt.Errorf("nothing to update — set --topic/--content/--visibility/--tag")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			e, err := commonplace.Update(cmd.Context(), c, args[0], in)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(e)
			}
			fmt.Fprintf(os.Stderr, "updated %s (topic %q, %s)\n", e.ID, e.Topic, e.Visibility)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&topic, "topic", "", "new topic")
	f.StringVar(&content, "content", "", "new content")
	f.StringVar(&visibility, "visibility", "", "org | private")
	f.StringArrayVar(&tags, "tag", nil, "replace the entry's tags (repeatable)")
	return cmd
}

func newDeleteCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id> --yes",
		Short: "Delete a knowledge entry (irreversible; requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("pass --yes to confirm deletion (irreversible)")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			if err := commonplace.Delete(cmd.Context(), c, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "deleted %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the irreversible delete")
	return cmd
}
```

- [ ] **Step 5: Run — expect PASS** — `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/cli/kb/ -v && go test ./... && go vet ./...`. `go run ./cmd/cw kb --help` lists store/search/list/update/delete.

- [ ] **Step 6: Commit** — `git add go.mod go.sum internal/cli/kb/ && git commit -m "cw kb: update + delete (commonplace CRUD complete)"` (trailer as above). Do NOT push.

---

## Task 3: README + gated live CRUD

**Files:** `README.md`, `internal/cli/kb/integration_test.go` (extend the #3 gated test, or add a sibling)

- [ ] **Step 1: README** — under `## Knowledge (commonplace)` add:
```markdown
    cw kb update <id> [--topic t] [--content c] [--visibility org|private] [--tag x]   # only the flags set; --tag replaces tags
    cw kb delete <id> --yes                                                            # irreversible hard-delete
```

- [ ] **Step 2: Gated live CRUD** — add `TestLiveKBCrud` to `internal/cli/kb/integration_test.go` (reuse the existing `liveSession` helper there — same `CW_IT_*` knowledge:* identity as #3's `TestLiveKB`): store an entry → `commonplace.Update` its topic → `List` and assert the topic changed → `commonplace.Delete` → `List` and assert the id is gone.

```go
func TestLiveKBCrud(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD (knowledge:* identity) to run the live kb CRUD test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	c, _ := liveSession(t, edge)
	ctx := context.Background()
	marker := "cwkbcrud-" + time.Now().Format("150405")

	stored, err := commonplace.Store(ctx, c, commonplace.StoreInput{Topic: marker, Content: "original " + marker, Visibility: "org"})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	newTopic := marker + "-updated"
	if _, err := commonplace.Update(ctx, c, stored.ID, commonplace.UpdateInput{Topic: &newTopic}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	entries, err := commonplace.List(ctx, c)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.ID == stored.ID {
			found = true
			if e.Topic != newTopic {
				t.Fatalf("topic not updated: %q", e.Topic)
			}
		}
	}
	if !found {
		t.Fatalf("entry %s missing after update", stored.ID)
	}
	if err := commonplace.Delete(ctx, c, stored.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	entries, err = commonplace.List(ctx, c)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	for _, e := range entries {
		if e.ID == stored.ID {
			t.Fatalf("entry %s still present after delete", stored.ID)
		}
	}
}
```
(Ensure imports `time` + `commonplace` are present in the file.)

- [ ] **Step 3: Offline suite** — `cd /Users/jacinta/Source/cw && go build ./... && go vet ./... && go test ./...` green; `TestLiveKBCrud` SKIPs without `CW_IT_*`.

- [ ] **Step 4: Commit** — `git add -A && git commit -m "cw: README kb update/delete + gated live CRUD test"` (trailer as above). Do NOT push.

- [ ] **Step 5: Controller — live smoke + merge.** Provision/reuse a `knowledge:*` identity (as in #3), run `TestLiveKBCrud` against dMon (or a manual `cw kb store → update → list → delete → list` smoke). Then PR + merge cw. (cwb-client already merged.)

---

## Self-review

**Spec coverage:** `commonplace.UpdateInput`+`Update`+`Delete` → Task 1; `cw kb update` (changed-flags → PATCH; fail-fast on none; --tag replaces; --json Entry) + `cw kb delete --yes` (required) → Task 2; README + gated live CRUD round-trip → Task 3. ✔
**Placeholder scan:** `<H2>` is the controller-supplied merged-lib hash; `liveSession` reuses #3's helper. No TBD.
**Type consistency:** `commonplace.{UpdateInput,Update,Delete}` mirror `Store`/`List` (`do`, `base+"/"+url.PathEscape(id)`, bare Entry / 2xx-only); `cw kb update/delete` use `cmd.Flags().Changed` + `cmdutil.Session`. Two-repo pin: cw `go get cwb-client@<merged H2>`.
