package human

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestHumanCreateWiring: create POSTs the right body, and --password-stdin
// triggers the follow-up password call with the piped secret.
func TestHumanCreateWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var paths []string
	var pwBody string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/orgs/o1/humans", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"display_name":"alice"`) || !strings.Contains(string(b), "knowledge:read") {
			t.Errorf("create body = %s", b)
		}
		_, _ = w.Write([]byte(`{"id":"h1","display_name":"alice","org":"o1"}`))
	})
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		pwBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"create", "--org", "o1", "--name", "alice", "--scope", "knowledge:read", "--password-stdin"})
	cmd.SetIn(strings.NewReader("s3cr3t-passphrase\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/herald/api/orgs/o1/humans" || paths[1] != "/herald/api/humans/h1/password" {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(pwBody, "s3cr3t-passphrase") {
		t.Fatalf("password body = %s", pwBody)
	}
}

// TestReadSecret: --password-stdin reads + trims a line; empty when required errors.
func TestReadSecret(t *testing.T) {
	got, err := readSecret(strings.NewReader("topsecret\n"), true, true)
	if err != nil || got != "topsecret" {
		t.Fatalf("stdin path: %q %v", got, err)
	}
	if _, err := readSecret(strings.NewReader("\n"), true, true); err == nil {
		t.Fatal("empty stdin + required should error")
	}
}
