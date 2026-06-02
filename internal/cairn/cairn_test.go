package cairn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

func stub(t *testing.T) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cairn/api/orgs/o1/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"r1","org":"o1","slug":"widgets","default_branch":"main"}`))
	})
	mux.HandleFunc("GET /cairn/api/orgs/o1/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"r1","org":"o1","slug":"widgets","default_branch":"main"}]`))
	})
	mux.HandleFunc("POST /cairn/api/orgs/o1/repos/widgets/pulls", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"7","repo":"widgets","source":"feat","target":"main","title":"T","state":"open","ledger_issue_key":"NEX-9"}`))
	})
	mux.HandleFunc("GET /cairn/api/orgs/o1/repos/widgets/pulls", func(w http.ResponseWriter, r *http.Request) {
		// state must reach the wire as a query param ("open" here; absent for the empty-state call).
		if got := r.URL.Query().Get("state"); got != "open" && got != "" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"7","repo":"widgets","source":"feat","target":"main","title":"T","state":"open","ledger_issue_key":"NEX-9"}]`))
	})
	mux.HandleFunc("GET /cairn/api/orgs/o1/repos/widgets/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"7","repo":"widgets","source":"feat","target":"main","title":"T","state":"open","ledger_issue_key":"NEX-9"}`))
	})
	mux.HandleFunc("POST /cairn/api/orgs/o1/repos/widgets/pulls/7/merge", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"7","state":"merged","target":"main","merged_sha":"abc123"}`))
	})
	mux.HandleFunc("POST /cairn/api/orgs/o1/repos/dup/pulls/9/merge", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"not a fast-forward"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestWrapper(t *testing.T) {
	c := stub(t)
	ctx := context.Background()

	rp, err := CreateRepo(ctx, c, "o1", "widgets")
	if err != nil || rp.Slug != "widgets" || rp.DefaultBranch != "main" {
		t.Fatalf("CreateRepo: %v %+v", err, rp)
	}
	repos, err := ListRepos(ctx, c, "o1")
	if err != nil || len(repos) != 1 || repos[0].Slug != "widgets" {
		t.Fatalf("ListRepos: %v %+v", err, repos)
	}
	pull, err := OpenPull(ctx, c, "o1", "widgets", OpenPullInput{Source: "feat", Target: "main", Title: "T", Project: "NEX"})
	if err != nil || pull.ID != "7" || pull.LedgerIssueKey != "NEX-9" {
		t.Fatalf("OpenPull: %v %+v", err, pull)
	}
	pulls, err := ListPulls(ctx, c, "o1", "widgets", "open")
	if err != nil || len(pulls) != 1 || pulls[0].State != "open" {
		t.Fatalf("ListPulls(open): %v %+v", err, pulls)
	}
	// Empty state omits the query param entirely (stub allows absent state).
	if _, err := ListPulls(ctx, c, "o1", "widgets", ""); err != nil {
		t.Fatalf("ListPulls(\"\"): %v", err)
	}
	got, err := GetPull(ctx, c, "o1", "widgets", "7")
	if err != nil || got.ID != "7" {
		t.Fatalf("GetPull: %v %+v", err, got)
	}
	res, err := MergePull(ctx, c, "o1", "widgets", "7")
	if err != nil || res.MergedSHA != "abc123" || res.State != "merged" {
		t.Fatalf("MergePull: %v %+v", err, res)
	}
	// 409 → error surfacing cairn's message.
	if _, err := MergePull(ctx, c, "o1", "dup", "9"); err == nil || !strings.Contains(err.Error(), "fast-forward") {
		t.Fatalf("merge 409 err = %v, want 'fast-forward'", err)
	}
}
