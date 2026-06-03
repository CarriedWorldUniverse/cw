package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cwb-client/identity"
	casket "github.com/CarriedWorldUniverse/casket-go"
)

// TestKeygen: output is valid base64-std decoding to 32 bytes, and two runs differ.
func TestKeygen(t *testing.T) {
	run := func() string {
		gf := &cmdutil.GlobalFlags{}
		cmd := NewCmd(gf)
		cmd.SetArgs([]string{"keygen"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("keygen: %v", err)
		}
		return strings.TrimSpace(out.String())
	}
	s1 := run()
	raw, err := base64.StdEncoding.DecodeString(s1)
	if err != nil || len(raw) != 32 {
		t.Fatalf("keygen output not 32-byte base64: %q (%d bytes, err %v)", s1, len(raw), err)
	}
	if s2 := run(); s2 == s1 {
		t.Fatal("two keygens produced the same seed")
	}
}

// TestCreateWiring: create derives the pubkey from CW_OWNER_SEED+slug and POSTs it.
func TestCreateWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	const seed, slug = "test-owner-seed", "builder"
	_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
	if err != nil {
		t.Fatal(err)
	}
	wantPub := base64.StdEncoding.EncodeToString(pub)

	var gotPubkey string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/agents", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPubkey = string(b)
		_, _ = w.Write([]byte(`{"id":"a1","kind":"agent","display_name":"builder","org":"o1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("CW_OWNER_SEED", seed)
	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"create", "--org", "o1", "--name", "builder", "--slug", slug,
		"--responsible-human", "h1", "--scope", "repo:read"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(gotPubkey, wantPub) {
		t.Fatalf("posted body %q missing derived pubkey %q", gotPubkey, wantPub)
	}
}

// TestCreateJSON: --json emits the full Agent as valid JSON to stdout.
func TestCreateJSON(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_OWNER_SEED", "test-owner-seed")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/agents", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"a1","kind":"agent","display_name":"builder","org":"o1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok", JSON: true}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"create", "--org", "o1", "--name", "builder", "--slug", "builder", "--responsible-human", "h1"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create --json: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("--json output not valid JSON: %v\nraw: %s", err, out.String())
	}
	if got["id"] != "a1" || got["kind"] != "agent" {
		t.Fatalf("unexpected --json payload: %v", got)
	}
}

// TestCreateRequiresSeed: create fails fast (no HTTP call) when CW_OWNER_SEED is unset.
func TestCreateRequiresSeed(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_OWNER_SEED", "")
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/agents", func(http.ResponseWriter, *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"create", "--org", "o1", "--name", "b", "--slug", "b", "--responsible-human", "h1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing-seed error")
	}
	if called {
		t.Fatal("create must not hit the server without CW_OWNER_SEED")
	}
}

func TestPubkey(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "cw-pubkey-test-seed")
	gf := &cmdutil.GlobalFlags{}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"pubkey", "--slug", "builder"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pubkey: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "YK99cMH64LflXEUjEHD38AMCrOStGKPE8uyj0GP28wI=") {
		t.Fatalf("missing pubkey:\n%s", s)
	}
	if !strings.Contains(s, "vhkj2Fplk7uTkGzGSKDEJQ") {
		t.Fatalf("missing fingerprint:\n%s", s)
	}
}

func TestPubkeyJSON(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "cw-pubkey-test-seed")
	gf := &cmdutil.GlobalFlags{JSON: true}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"pubkey", "--slug", "builder"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pubkey --json: %v", err)
	}
	var got struct{ Slug, Pubkey, Fingerprint string }
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if got.Slug != "builder" || got.Fingerprint != "vhkj2Fplk7uTkGzGSKDEJQ" ||
		got.Pubkey != "YK99cMH64LflXEUjEHD38AMCrOStGKPE8uyj0GP28wI=" {
		t.Fatalf("json: %+v", got)
	}
	// Cross-check against a direct derivation.
	_, pub, _ := casket.DeriveAgentKey([]byte("cw-pubkey-test-seed"), "builder")
	if got.Fingerprint != identity.Fingerprint(pub) {
		t.Fatalf("fingerprint mismatch vs identity.Fingerprint")
	}
}

func TestPubkeyRequiresSeed(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "")
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"pubkey", "--slug", "builder"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing-seed error")
	}
}

func TestPubkeyRequiresSlug(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "cw-pubkey-test-seed")
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"pubkey"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing-slug error")
	}
}
