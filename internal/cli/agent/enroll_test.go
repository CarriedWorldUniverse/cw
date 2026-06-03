package agent

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	casket "github.com/CarriedWorldUniverse/casket-go"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cwb-client/identity"
)

const enrollOwnerSeed = "test-owner-seed-deadbeef-cafef00d"

func runEnroll(t *testing.T, edge, out string, args ...string) error {
	t.Helper()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	gf := &cmdutil.GlobalFlags{Edge: edge, Token: "tok"}
	cmd := newEnrollCmd(gf)
	cmd.SetArgs(append([]string{"--url", "ws://nexus.local/connect", "--out", out}, args...))
	return cmd.Execute()
}

func TestEnrollAttachWritesKeyfile(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", enrollOwnerSeed)
	priv, pub, _ := casket.DeriveAgentKey([]byte(enrollOwnerSeed), "plumb")
	fp := identity.Fingerprint(pub)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/herald/api/agents/by-fingerprint/"+fp {
			_, _ = w.Write([]byte(`{"id":"agent-uuid-9","kind":"agent","display_name":"plumb","org":"o1","fingerprint":"` + fp + `","status":"active","active":true}`))
			return
		}
		http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "plumb.keyfile.json")
	if err := runEnroll(t, srv.URL, out, "--slug", "plumb"); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read keyfile: %v", err)
	}
	var kf struct {
		Key         string `json:"key"`
		KeyID       string `json:"key_id"`
		URL         string `json:"url"`
		Slug        string `json:"slug"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(raw, &kf); err != nil {
		t.Fatalf("parse keyfile: %v", err)
	}
	if kf.KeyID != "agent-uuid-9" || kf.Slug != "plumb" || kf.Fingerprint != fp || kf.URL != "ws://nexus.local/connect" {
		t.Fatalf("keyfile = %+v", kf)
	}
	// key must be the base64 of the derived private key.
	if kf.Key != base64.StdEncoding.EncodeToString(priv) {
		t.Fatalf("key mismatch")
	}
	if info, _ := os.Stat(out); info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %v, want 0600", info.Mode().Perm())
	}
}

func TestEnrollNotFoundAborts(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", enrollOwnerSeed)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no agent for fingerprint", http.StatusNotFound)
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "typo.keyfile.json")
	if err := runEnroll(t, srv.URL, out, "--slug", "plubm"); err == nil {
		t.Fatal("expected abort on not-found")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("keyfile should not be written on not-found")
	}
}

func TestEnrollRequiresSeed(t *testing.T) {
	os.Unsetenv("CW_OWNER_SEED")
	out := filepath.Join(t.TempDir(), "x.keyfile.json")
	if err := runEnroll(t, "http://unused", out, "--slug", "plumb"); err == nil {
		t.Fatal("expected error when CW_OWNER_SEED unset")
	}
}
