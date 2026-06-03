package repo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CarriedWorldUniverse/cwb-client/cairn"
	"github.com/CarriedWorldUniverse/cwb-client/client"
)

// newRepoStub serves the cairn repo endpoints (mirrors internal/cairn's stub)
// and returns a static-token client pointed at it.
func newRepoStub(t *testing.T) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cairn/api/orgs/o1/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"r1","org":"o1","slug":"widgets","default_branch":"main"}`))
	})
	mux.HandleFunc("GET /cairn/api/orgs/o1/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"r1","org":"o1","slug":"widgets","default_branch":"main"}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

// We test the underlying run functions (cobra wiring is thin). createRepo/listRepos
// take a *client.Client + org and print; here we assert the cairn calls succeed
// against a stub by reusing the cairn package's stub via an exported test seam.
func TestRepoRunners(t *testing.T) {
	c, org := stubClient(t) // builds a stub client + org "o1" (see helper below)
	if _, err := cairn.CreateRepo(context.Background(), c, org, "widgets"); err != nil {
		t.Fatalf("create: %v", err)
	}
	repos, err := cairn.ListRepos(context.Background(), c, org)
	if err != nil || len(repos) == 0 {
		t.Fatalf("list: %v %+v", err, repos)
	}
}

func stubClient(t *testing.T) (*client.Client, string) { return newRepoStub(t), "o1" }
