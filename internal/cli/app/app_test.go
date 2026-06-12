package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

func TestPrecheckDeclaration(t *testing.T) {
	if err := precheck("lynxai", []byte("name: lynxai\nnamespace: nexus\nimage: x")); err != nil {
		t.Fatal(err)
	}
	if err := precheck("lynxai", []byte("name: other\nnamespace: nexus\nimage: x")); err == nil {
		t.Fatal("want name-mismatch error")
	}
	if err := precheck("lynxai", []byte("namespace: nexus")); err == nil {
		t.Fatal("want missing-fields error")
	}
	if err := precheck("lynxai", []byte(":::bad")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestPhaseDisplay(t *testing.T) {
	for in, want := range map[string]string{
		"APP_PHASE_SYNCED":      "Synced",
		"APP_PHASE_PROGRESSING": "Progressing",
		"APP_PHASE_DEGRADED":    "Degraded",
		"APP_PHASE_INVALID":     "Invalid",
		"APP_PHASE_UNKNOWN":     "Unknown",
		"APP_PHASE_UNSPECIFIED": "Unknown",
		"":                      "Unknown",
		"APP_PHASE_FUTURE":      "APP_PHASE_FUTURE", // unrecognized: pass through verbatim
	} {
		if got := phaseDisplay(in); got != want {
			t.Errorf("phaseDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}

// run executes a cw app subcommand against gf and returns combined output.
func run(t *testing.T, gf *cmdutil.GlobalFlags, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := New(gf)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// edgeFixture stands up an httptest edge serving the mason+almanac gateway
// routes and returns GlobalFlags pointed at it with a static token.
func edgeFixture(t *testing.T, mux *http.ServeMux) *cmdutil.GlobalFlags {
	t.Helper()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_APP_TLS_CERT", "") // edge transport must be the no-env default
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"}
}

const lynxaiSynced = `{"name":"lynxai","namespace":"nexus","phase":"APP_PHASE_SYNCED","message":"ok","ready":"1/1","decl_hash":"abc","applied_hash":"abc","last_applied_at":"2026-06-11T00:00:00Z","last_checked_at":"2026-06-11T00:01:00Z"}`

func TestLsViaEdge(t *testing.T) {
	var auth string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mason/api/apps", func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"apps":[` + lynxaiSynced + `]}`))
	})
	out, err := run(t, edgeFixture(t, mux), "ls")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if auth != "Bearer tok" {
		t.Fatalf("Authorization = %q, want bearer", auth)
	}
	for _, want := range []string{"lynxai", "nexus", "Synced", "1/1"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "APP_PHASE") {
		t.Errorf("ls output leaks raw enum:\n%s", out)
	}
}

func TestStatusViaEdge(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mason/api/apps/lynxai", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"app":` + lynxaiSynced + `,"declaration":"name: lynxai\n"}`))
	})
	out, err := run(t, edgeFixture(t, mux), "status", "lynxai")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"phase:        Synced", "decl hash:    abc", "--- declaration ---", "name: lynxai"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestSyncViaEdge(t *testing.T) {
	var body []byte
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mason/api/apps:sync", func(w http.ResponseWriter, r *http.Request) {
		body, _ = readAll(r)
		_, _ = w.Write([]byte(`{"apps":[` + lynxaiSynced + `]}`))
	})
	out, err := run(t, edgeFixture(t, mux), "sync", "lynxai")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Name != "lynxai" {
		t.Fatalf("sync body = %s (err %v), want {\"name\":\"lynxai\"}", body, err)
	}
	if !strings.Contains(out, "lynxai: Synced 1/1") {
		t.Errorf("sync output:\n%s", out)
	}
}

func TestSyncAllViaEdge(t *testing.T) {
	var body []byte
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mason/api/apps:sync", func(w http.ResponseWriter, r *http.Request) {
		body, _ = readAll(r)
		_, _ = w.Write([]byte(`{"apps":[]}`))
	})
	if _, err := run(t, edgeFixture(t, mux), "sync"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "{}" {
		t.Fatalf("sync-all body = %q, want {}", got)
	}
}

func TestDeclareViaEdge(t *testing.T) {
	var method, path string
	var body []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/almanac/", func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		body, _ = readAll(r)
		_, _ = w.Write([]byte(`{"item":{"path":"cwb/mason/apps/lynxai","version":2}}`))
	})
	yaml := "name: lynxai\nnamespace: nexus\nimage: x\n"
	file := filepath.Join(t.TempDir(), "lynxai.yaml")
	if err := os.WriteFile(file, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, edgeFixture(t, mux), "declare", file)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	// Exact grpc-gateway SetConfig binding: PUT /api/config/{path=**}, body "*"
	// (path bound from the URL, so the body carries only value).
	if method != http.MethodPut || path != "/almanac/api/config/cwb/mason/apps/lynxai" {
		t.Fatalf("declare hit %s %s, want PUT /almanac/api/config/cwb/mason/apps/lynxai", method, path)
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("declare body %s: %v", body, err)
	}
	if req["value"] != yaml || len(req) != 1 {
		t.Fatalf("declare body = %s, want {\"value\":<yaml>} only", body)
	}
	if !strings.Contains(out, "declared lynxai -> cwb/mason/apps/lynxai") {
		t.Errorf("declare output:\n%s", out)
	}
}

func TestRmViaEdge(t *testing.T) {
	var method, path string
	mux := http.NewServeMux()
	mux.HandleFunc("/almanac/", func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	})
	out, err := run(t, edgeFixture(t, mux), "rm", "lynxai")
	if err != nil {
		t.Fatalf("rm: %v", err)
	}
	if method != http.MethodDelete || path != "/almanac/api/config/cwb/mason/apps/lynxai" {
		t.Fatalf("rm hit %s %s, want DELETE /almanac/api/config/cwb/mason/apps/lynxai", method, path)
	}
	if !strings.Contains(out, "removed cwb/mason/apps/lynxai") {
		t.Errorf("rm output:\n%s", out)
	}
}

func TestMissingScopeIsCleanError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mason/api/apps:sync", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":7,"message":"missing scope app:write","details":[]}`))
	})
	_, err := run(t, edgeFixture(t, mux), "sync", "lynxai")
	if err == nil {
		t.Fatal("want scope error")
	}
	if !strings.Contains(err.Error(), "missing scope app:write") {
		t.Fatalf("error = %q, want it to carry the scope message", err)
	}
	if strings.ContainsAny(err.Error(), "{}") {
		t.Fatalf("error leaks raw JSON: %q", err)
	}
}

func TestUnauthorizedIsCleanError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mason/api/apps", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":16,"message":"invalid token"}`))
	})
	_, err := run(t, edgeFixture(t, mux), "ls")
	if err == nil {
		t.Fatal("want auth error")
	}
	if !strings.Contains(err.Error(), "invalid token") || strings.ContainsAny(err.Error(), "{}") {
		t.Fatalf("error = %q, want clean message", err)
	}
}

// TestTLSEnvSelectsDirectGRPC: setting CW_APP_TLS_CERT opts into the in-mesh
// break-glass transport — the edge must NOT be contacted, and the incomplete
// TLS env must error mentioning the CW_APP_TLS contract.
func TestTLSEnvSelectsDirectGRPC(t *testing.T) {
	hit := false
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { hit = true })
	gf := edgeFixture(t, mux)
	t.Setenv("CW_APP_TLS_CERT", "/nonexistent/cert.pem")
	_, err := run(t, gf, "ls")
	if err == nil || !strings.Contains(err.Error(), "CW_APP_TLS") {
		t.Fatalf("want CW_APP_TLS error, got %v", err)
	}
	if hit {
		t.Fatal("edge was contacted despite CW_APP_TLS_CERT being set")
	}
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}
