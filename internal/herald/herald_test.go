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
	mux.HandleFunc("POST /herald/api/orgs/o1/products/ledger/enable", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			t.Errorf("enable must send no body, got %d bytes", r.ContentLength)
		}
		_, _ = w.Write([]byte(`{"cairn":true,"ledger":true,"commonplace":true}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/products/ledger/disable", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			t.Errorf("disable must send no body, got %d bytes", r.ContentLength)
		}
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
	// DeleteOrg (body on DELETE) → {"message":...} branch of errMsg.
	mux.HandleFunc("DELETE /herald/api/orgs/o1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"org not found"}`))
	})
	// SetHumanPassword (nil out) → server message surfaces on a 2xx-only call.
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"password too short"}`))
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
	// {"message":...} branch (DeleteOrg sends a body on DELETE).
	if _, err := DeleteOrg(ctx, c, "o1", "acme"); err == nil ||
		!strings.Contains(err.Error(), "org not found") {
		t.Fatalf("DeleteOrg error: want message branch, got %v", err)
	}
	// nil-out call still surfaces the server error.
	if err := SetHumanPassword(ctx, c, "h1", "short"); err == nil ||
		!strings.Contains(err.Error(), "password too short") {
		t.Fatalf("SetHumanPassword error: want server message, got %v", err)
	}
}

func TestCreateAgent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/agents", func(w http.ResponseWriter, r *http.Request) {
		body := decode(t, r)
		if body["display_name"] != "builder" || body["responsible_human"] != "h1" ||
			body["casket_pubkey"] != "cHVia2V5" || body["org"] != nil {
			t.Errorf("create agent body = %v (want dn/responsible_human/casket_pubkey, no org)", body)
		}
		_, _ = w.Write([]byte(`{"id":"a1","kind":"agent","display_name":"builder","org":"o1","responsible_human":"h1","fingerprint":"fp1","status":"active","active":true,"scopes":["repo:read"]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.WithStaticToken(srv.URL, "tok")

	a, err := CreateAgent(context.Background(), c, "o1", CreateAgentInput{
		DisplayName: "builder", ResponsibleHuman: "h1", CasketPubkey: "cHVia2V5", Scopes: []string{"repo:read"},
	})
	if err != nil || a.ID != "a1" || a.Kind != "agent" || a.Fingerprint != "fp1" || !a.Active || len(a.Scopes) != 1 {
		t.Fatalf("CreateAgent: %v %+v", err, a)
	}
}

func TestMe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, r *http.Request) {
		// Return a human record (empty agent fields) the first call, an agent the second.
		if r.Header.Get("X-Want") == "agent" {
			_, _ = w.Write([]byte(`{"id":"a1","kind":"agent","display_name":"builder","org":"o1","org_name":"acme","status":"active","scopes":["repo:read"],"responsible_human":"h1","fingerprint":"SHA256:zzz"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"h1","kind":"human","display_name":"alice@x","org":"o1","org_name":"acme","status":"active","scopes":["issue:read"]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.WithStaticToken(srv.URL, "tok")

	hu, err := Me(context.Background(), c)
	if err != nil || hu.ID != "h1" || hu.Kind != "human" || hu.OrgName != "acme" || hu.Status != "active" {
		t.Fatalf("Me(human): %v %+v", err, hu)
	}
	if hu.ResponsibleHuman != "" || hu.Fingerprint != "" {
		t.Fatalf("human should have no agent fields: %+v", hu)
	}
	if len(hu.Scopes) != 1 || hu.Scopes[0] != "issue:read" {
		t.Fatalf("human scopes: %v", hu.Scopes)
	}
}

func TestMeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing identity"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.WithStaticToken(srv.URL, "tok")
	if _, err := Me(context.Background(), c); err == nil || !strings.Contains(err.Error(), "missing identity") {
		t.Fatalf("Me error: want server message, got %v", err)
	}
}
