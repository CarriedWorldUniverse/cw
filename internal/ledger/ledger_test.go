package ledger

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
	mux.HandleFunc("POST /ledger/api/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"NEX-9","project":"NEX","type":"Story","status":"To Do","summary":"S"}`))
	})
	mux.HandleFunc("GET /ledger/api/issues/my", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"key":"NEX-9","status":"To Do","summary":"S"}]}`))
	})
	mux.HandleFunc("GET /ledger/api/issues/ready", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"key":"NEX-9","status":"To Do","summary":"S"}]}`))
	})
	mux.HandleFunc("POST /ledger/api/issues/search", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"refs":[{"key":"NEX-9","project":"NEX","status":"To Do","summary":"S"}]}`))
	})
	mux.HandleFunc("GET /ledger/api/issues/NEX-9", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"NEX-9","project":"NEX","type":"Story","status":"To Do","summary":"S"}`))
	})
	mux.HandleFunc("POST /ledger/api/issues/NEX-9/claim", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"NEX-9","status":"In Progress","summary":"S"}`))
	})
	mux.HandleFunc("POST /ledger/api/issues/NEX-9/transition", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /ledger/api/issues/NEX-9/comments", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /ledger/api/issues/NEX-1/transition", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"definition of done not met"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestWrapper(t *testing.T) {
	c := stub(t)
	ctx := context.Background()

	iss, err := CreateIssue(ctx, c, CreateInput{Project: "NEX", Type: "Story", Summary: "S"})
	if err != nil || iss.Key != "NEX-9" || iss.Project != "NEX" {
		t.Fatalf("CreateIssue: %v %+v", err, iss)
	}
	mine, err := ListMine(ctx, c)
	if err != nil || len(mine) != 1 || mine[0].Key != "NEX-9" {
		t.Fatalf("ListMine: %v %+v", err, mine)
	}
	ready, err := ListReady(ctx, c)
	if err != nil || len(ready) != 1 {
		t.Fatalf("ListReady: %v %+v", err, ready)
	}
	refs, err := SearchByProject(ctx, c, "NEX")
	if err != nil || len(refs) != 1 || refs[0].Project != "NEX" {
		t.Fatalf("SearchByProject: %v %+v", err, refs)
	}
	got, err := GetIssue(ctx, c, "NEX-9")
	if err != nil || got.Key != "NEX-9" {
		t.Fatalf("GetIssue: %v %+v", err, got)
	}
	claimed, err := Claim(ctx, c, "NEX-9")
	if err != nil || claimed.Status != "In Progress" {
		t.Fatalf("Claim: %v %+v", err, claimed)
	}
	if err := Transition(ctx, c, "NEX-9", "In Review"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if err := Comment(ctx, c, "NEX-9", "looks good"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	// DoD-gate 400 surfaces ledger's message.
	if err := Transition(ctx, c, "NEX-1", "Done"); err == nil || !strings.Contains(err.Error(), "definition of done") {
		t.Fatalf("DoD-gate err = %v, want 'definition of done'", err)
	}
}
