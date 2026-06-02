package herald

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

func decode(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	if len(b) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("bad request body %q: %v", b, err)
	}
	return m
}

func stub(t *testing.T) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["name"] != "acme" {
			t.Errorf("create org body = %v", body)
		}
		_, _ = w.Write([]byte(`{"id":"o1","name":"acme"}`)) // bare Org (response_body:"org")
	})
	mux.HandleFunc("GET /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"orgs":[{"id":"o1","name":"acme"}]}`))
	})
	mux.HandleFunc("DELETE /herald/api/orgs/o1", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["name"] != "acme" || body["id"] != nil {
			t.Errorf("delete body = %v (want only name)", body)
		}
		_, _ = w.Write([]byte(`{"deleted":"o1","pillars":["cairn","ledger"]}`))
	})
	mux.HandleFunc("GET /herald/api/orgs/o1/products", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":false,"commonplace":true}`)) // bare map
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/products/ledger/enable", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":true,"commonplace":true}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/products/ledger/disable", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":false,"commonplace":true}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/humans", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["display_name"] != "alice" || body["org"] != nil {
			t.Errorf("create human body = %v (want display_name+scopes, no org)", body)
		}
		_, _ = w.Write([]byte(`{"id":"h1","display_name":"alice","org":"o1"}`)) // bare Human
	})
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["password"] != "hunter2hunter2" {
			t.Errorf("set-password body = %v", body)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestWrapper(t *testing.T) {
	c := stub(t)
	ctx := context.Background()

	o, err := CreateOrg(ctx, c, CreateOrgInput{Name: "acme"})
	if err != nil || o.ID != "o1" || o.Name != "acme" {
		t.Fatalf("CreateOrg: %v %+v", err, o)
	}
	orgs, err := ListOrgs(ctx, c)
	if err != nil || len(orgs) != 1 || orgs[0].ID != "o1" {
		t.Fatalf("ListOrgs: %v %+v", err, orgs)
	}
	del, err := DeleteOrg(ctx, c, "o1", "acme")
	if err != nil || del.Deleted != "o1" || len(del.Pillars) != 2 {
		t.Fatalf("DeleteOrg: %v %+v", err, del)
	}
	prods, err := GetProducts(ctx, c, "o1")
	if err != nil || prods["ledger"] != false || prods["cairn"] != true {
		t.Fatalf("GetProducts: %v %+v", err, prods)
	}
	en, err := EnableProduct(ctx, c, "o1", "ledger")
	if err != nil || en["ledger"] != true {
		t.Fatalf("EnableProduct: %v %+v", err, en)
	}
	dis, err := DisableProduct(ctx, c, "o1", "ledger")
	if err != nil || dis["ledger"] != false {
		t.Fatalf("DisableProduct: %v %+v", err, dis)
	}
	h, err := CreateHuman(ctx, c, "o1", CreateHumanInput{DisplayName: "alice", Scopes: []string{"knowledge:read"}})
	if err != nil || h.ID != "h1" || h.Org != "o1" {
		t.Fatalf("CreateHuman: %v %+v", err, h)
	}
	if err := SetHumanPassword(ctx, c, "h1", "hunter2hunter2"); err != nil {
		t.Fatalf("SetHumanPassword: %v", err)
	}
}

func errStub(t *testing.T) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"requires herald:platform-admin"}`))
	})
	mux.HandleFunc("GET /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.WithStaticToken(srv.URL, "tok")
}

func TestErrorMapping(t *testing.T) {
	c := errStub(t)
	ctx := context.Background()
	if _, err := CreateOrg(ctx, c, CreateOrgInput{Name: "x"}); err == nil ||
		!strings.Contains(err.Error(), "requires herald:platform-admin") {
		t.Fatalf("CreateOrg error: want server message, got %v", err)
	}
	if _, err := ListOrgs(ctx, c); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("ListOrgs error: want status fallback, got %v", err)
	}
}
