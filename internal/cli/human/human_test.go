package human

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
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

// TestHumanSetPasswordWiring: set-password --password-stdin posts the piped
// secret to /humans/{id}/password.
func TestHumanSetPasswordWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var gotBody string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"set-password", "h1", "--password-stdin"})
	cmd.SetIn(strings.NewReader("newpass-passphrase\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(gotBody, "newpass-passphrase") {
		t.Fatalf("password body = %s", gotBody)
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

func TestHumanPasswordSetWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	restorePrompt := stubConfirmedPassword(t, "new-password")
	defer restorePrompt()

	var gotAuth, gotBody string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"password", "set", "--id", "h1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotBody != `{"password":"new-password"}` {
		t.Fatalf("password body = %s", gotBody)
	}
}

func TestHumanPasswordSetUnauthorized(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	restorePrompt := stubConfirmedPassword(t, "new-password")
	defer restorePrompt()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/humans/h1/password", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"password", "set", "--id", "h1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("execute should fail")
	}
	if !strings.Contains(err.Error(), "unauthorized (401)") || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error = %v", err)
	}
}

func TestHumanPasswordSetMismatchAborts(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	restorePrompt := stubPasswordReads(t, "new-password", "other-password")
	defer restorePrompt()

	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"password", "set", "--id", "h1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("execute should fail")
	}
	if !strings.Contains(err.Error(), "passwords do not match") {
		t.Fatalf("error = %v", err)
	}
	if hit {
		t.Fatal("server should not be called on mismatch")
	}
}

func TestHumanPasswordSetRejectsShortPassword(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	restorePrompt := stubPasswordReads(t, "short")
	defer restorePrompt()

	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"password", "set", "--id", "h1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("execute should fail")
	}
	if !strings.Contains(err.Error(), "at least 8 characters") {
		t.Fatalf("error = %v", err)
	}
	if hit {
		t.Fatal("server should not be called for a short password")
	}
}

func TestHumanPasswordSetDefaultsIDToCallerSubject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CW_CONFIG_DIR", dir)
	if err := (&config.Config{
		CurrentContext: "dev",
		Contexts: map[string]config.Context{
			"dev": {Edge: "http://unused", Identity: config.Identity{Kind: "human", Subject: "self-human", Display: "alice"}},
		},
	}).Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}
	restorePrompt := stubConfirmedPassword(t, "new-password")
	defer restorePrompt()

	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /herald/api/humans/self-human/password", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gf := &cmdutil.GlobalFlags{Edge: srv.URL, Token: "not-a-jwt"}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"password", "set"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/herald/api/humans/self-human/password" {
		t.Fatalf("path = %q", gotPath)
	}
}

func stubConfirmedPassword(t *testing.T, password string) func() {
	t.Helper()
	return stubPasswordReads(t, password, password)
}

func stubPasswordReads(t *testing.T, values ...string) func() {
	t.Helper()
	old := promptConfirmedPassword
	promptConfirmedPassword = func() (string, error) {
		i := 0
		return readConfirmedPassword(func(string) (string, error) {
			if i >= len(values) {
				t.Fatalf("prompt read %d exceeds stub values %d", i+1, len(values))
			}
			v := values[i]
			i++
			return v, nil
		})
	}
	return func() { promptConfirmedPassword = old }
}
