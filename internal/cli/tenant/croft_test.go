package tenant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	cwkeyfile "github.com/CarriedWorldUniverse/cw/internal/keyfile"
	"github.com/CarriedWorldUniverse/cwb-client/client"
)

// fakeSecretWriter records the secrets provisionCroft writes, without a cluster.
type fakeSecretWriter struct {
	written map[string]map[string][]byte // name -> data
}

func (f *fakeSecretWriter) WriteSecret(_ context.Context, _ /*ns*/, name string, data map[string][]byte) error {
	if f.written == nil {
		f.written = map[string]map[string][]byte{}
	}
	f.written[name] = data
	return nil
}

// TestOnboardProvisionsCroft: with --provision-croft, onboard registers the
// croft (via the injected writer) and reports it in the result.
func TestOnboardProvisionsCroft(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"orgs":[]}`))
	})
	mux.HandleFunc("POST /herald/api/orgs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"o1","name":"acme"}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/humans", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u1","display_name":"alice@acme.test","org":"o1"}`))
	})
	mux.HandleFunc("POST /herald/api/orgs/o1/agents", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"croft-1","kind":"agent","display_name":"croft","org":"o1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Inject a fake secret writer for the duration of the test.
	sw := &fakeSecretWriter{}
	prev := croftSecretWriter
	croftSecretWriter = sw
	t.Cleanup(func() { croftSecretWriter = prev })

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok", JSON: true}
	cmd := NewCmd(gf)
	out := &strings.Builder{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"onboard", "acme", "--owner", "alice@acme.test", "--provision-croft"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var res onboardResult
	if err := json.Unmarshal([]byte(out.String()), &res); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	if !res.CroftProvisioned || res.CroftAgent != "croft-1" {
		t.Fatalf("croft not provisioned in result: %+v", res)
	}
	if _, ok := sw.written["aspect-keyfile-croft-o1"]; !ok {
		t.Fatalf("croft keyfile secret not written: %v", sw.written)
	}
}

// TestProvisionCroft registers an org-scoped croft agent (role:croft, owner as
// responsible human) and writes the seed + bootstrap keyfile secrets, never
// surfacing the seed in its result.
func TestProvisionCroft(t *testing.T) {
	var agentBody struct {
		DisplayName      string   `json:"display_name"`
		ResponsibleHuman string   `json:"responsible_human"`
		CasketPubkey     string   `json:"casket_pubkey"`
		Scopes           []string `json:"scopes"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/agents", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &agentBody)
		_, _ = w.Write([]byte(`{"id":"croft-1","kind":"agent","display_name":"croft","org":"o1","scopes":["repo:read"]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := client.WithStaticToken(srv.URL, "tok")
	sw := &fakeSecretWriter{}
	res, err := provisionCroft(context.Background(), c, sw, "o1", "owner-1", "https://broker.example:7888", "nexus")
	if err != nil {
		t.Fatalf("provisionCroft: %v", err)
	}

	// Agent registration: org-scoped croft role, owner responsible, real pubkey.
	if len(agentBody.Scopes) != 1 || agentBody.Scopes[0] != "role:croft" {
		t.Fatalf("croft must be granted role:croft, got %v", agentBody.Scopes)
	}
	if agentBody.ResponsibleHuman != "owner-1" {
		t.Fatalf("responsible_human = %q, want owner-1", agentBody.ResponsibleHuman)
	}
	if agentBody.DisplayName != "croft" {
		t.Fatalf("display_name = %q", agentBody.DisplayName)
	}
	if raw, derr := base64.StdEncoding.DecodeString(agentBody.CasketPubkey); derr != nil || len(raw) != 32 {
		t.Fatalf("casket_pubkey not a 32-byte std-b64 key: err=%v len=%d", derr, len(raw))
	}

	// Result carries the agent id + fingerprint, NOT the seed.
	if res.AgentID != "croft-1" {
		t.Fatalf("AgentID = %q", res.AgentID)
	}
	if res.Fingerprint == "" {
		t.Fatal("missing fingerprint")
	}

	// Both secrets written, with the expected keys + non-empty material.
	seed := sw.written["croft-seed-o1"]
	if len(seed["seed"]) == 0 {
		t.Fatalf("croft-seed-o1 not written with a seed: %v", sw.written)
	}
	kf := sw.written["aspect-keyfile-croft-o1"]
	if len(kf["keyfile.json"]) == 0 {
		t.Fatalf("aspect-keyfile-croft-o1 not written: %v", sw.written)
	}
	// The keyfile binds the herald agent id + the broker seam + slug "croft".
	var parsed cwkeyfile.Bootstrap
	if err := json.Unmarshal(kf["keyfile.json"], &parsed); err != nil {
		t.Fatalf("keyfile not valid JSON: %v", err)
	}
	if parsed.KeyID != "croft-1" || parsed.Slug != "croft" || parsed.URL != "https://broker.example:7888" || parsed.Key == "" {
		t.Fatalf("keyfile fields wrong: %+v", parsed)
	}

	// The seed must never leak into any user-facing result string.
	if strings.Contains(res.AgentID+res.Fingerprint, string(seed["seed"])) {
		t.Fatal("seed leaked into the result")
	}
}
