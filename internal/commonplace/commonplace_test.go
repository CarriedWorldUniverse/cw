package commonplace

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
	mux.HandleFunc("POST /knowledge/api/knowledge", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"e1","org":"o1","owner":"u1","topic":"onboarding","content":"hello","visibility":"org"}`))
	})
	mux.HandleFunc("GET /knowledge/api/knowledge/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" || r.URL.Query().Get("top_k") == "" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"hits":[{"entry":{"id":"e1","topic":"onboarding","content":"hello"},"score":0.91}]}`))
	})
	mux.HandleFunc("GET /knowledge/api/knowledge", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"entries":[{"id":"e1","topic":"onboarding","visibility":"org"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestWrapper(t *testing.T) {
	c := stub(t)
	ctx := context.Background()

	e, err := Store(ctx, c, StoreInput{Topic: "onboarding", Content: "hello", Visibility: "org"})
	if err != nil || e.ID != "e1" || e.Topic != "onboarding" {
		t.Fatalf("Store: %v %+v", err, e)
	}
	hits, err := Search(ctx, c, "where do we deploy", 5)
	if err != nil || len(hits) != 1 || hits[0].Entry.ID != "e1" || hits[0].Score == 0 {
		t.Fatalf("Search: %v %+v", err, hits)
	}
	entries, err := List(ctx, c)
	if err != nil || len(entries) != 1 || entries[0].ID != "e1" {
		t.Fatalf("List: %v %+v", err, entries)
	}
}

// errStub serves a non-2xx JSON {"error":...} on store, and a body-less 500 on
// list, so both errMsg branches (server message + status fallback) are covered.
func errStub(t *testing.T) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /knowledge/api/knowledge", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"missing scope knowledge:write"}`))
	})
	mux.HandleFunc("GET /knowledge/api/knowledge", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestErrorMapping(t *testing.T) {
	c := errStub(t)
	ctx := context.Background()

	if _, err := Store(ctx, c, StoreInput{Topic: "t", Content: "x"}); err == nil ||
		!strings.Contains(err.Error(), "missing scope knowledge:write") {
		t.Fatalf("Store error: want server message, got %v", err)
	}
	if _, err := List(ctx, c); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("List error: want status fallback, got %v", err)
	}
}
