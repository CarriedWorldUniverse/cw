package cred

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	casket "github.com/CarriedWorldUniverse/casket-go"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

func TestPersonalPutGetListRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CW_SATCHEL_DIR", dir)
	restore := stubPassphrase(t, "fixed test passphrase")
	defer restore()

	put := NewCmd(&cmdutil.GlobalFlags{})
	put.SetArgs([]string{"put", "personal/api-token"})
	put.SetIn(strings.NewReader("secret-value\n"))
	if err := put.Execute(); err != nil {
		t.Fatalf("put: %v", err)
	}

	var got bytes.Buffer
	get := NewCmd(&cmdutil.GlobalFlags{})
	get.SetArgs([]string{"get", "personal/api-token"})
	get.SetOut(&got)
	if err := get.Execute(); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.String() != "secret-value\n" {
		t.Fatalf("secret = %q", got.String())
	}

	var listed bytes.Buffer
	ls := NewCmd(&cmdutil.GlobalFlags{})
	ls.SetArgs([]string{"ls", "personal"})
	ls.SetOut(&listed)
	if err := ls.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if listed.String() != "api-token\n" {
		t.Fatalf("ls = %q", listed.String())
	}

	onDisk, err := os.ReadFile(filepath.Join(dir, "api-token.casket.json"))
	if err != nil {
		t.Fatalf("read stored envelope: %v", err)
	}
	if bytes.Contains(onDisk, []byte("secret-value")) {
		t.Fatalf("stored envelope contains plaintext secret: %s", onDisk)
	}
	if len(onDisk) == 0 || onDisk[0] != byte(casket.SuiteXChaCha20) {
		t.Fatalf("stored envelope is not a default casket blob: %x", onDisk)
	}
}

func TestWrongPassphraseAndMissingNameAreDistinct(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Put("api-token", []byte("secret-value"), "right passphrase"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := store.Get("api-token", "wrong passphrase"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong passphrase err = %v, want ErrDecrypt", err)
	}
	if _, err := store.Get("missing", "right passphrase"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
	}
}

func TestStoredBlobIsBoundToSecretName(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Put("api-token", []byte("secret-value"), "right passphrase"); err != nil {
		t.Fatalf("put: %v", err)
	}
	blob, err := os.ReadFile(filepath.Join(dir, "api-token.casket.json"))
	if err != nil {
		t.Fatalf("read stored envelope: %v", err)
	}
	key, err := secretKey("right passphrase")
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	if _, _, err := casket.Open(key, blob, []byte(repoIdentity), []byte("other-token")); err == nil {
		t.Fatal("open with wrong object path succeeded")
	}
}

func TestBareNameErrorShowsBothForms(t *testing.T) {
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"get", "api-token"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("bare name should fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "personal/api-token") || !strings.Contains(msg, "<org>/api-token") {
		t.Fatalf("bare-name help = %q", msg)
	}
}

func TestRemoteGetWiring(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())

	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /custodian/api/secret/cwb/meshy-api-key", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"path":"cwb/meshy-api-key","value":"secret-value"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var out bytes.Buffer
	cmd := NewCmd(&cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"})
	cmd.SetArgs([]string{"get", "cwb/meshy-api-key"})
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.String() != "secret-value" {
		t.Fatalf("secret = %q", out.String())
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestRemoteGetForbidden(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /custodian/api/secret/cwb/meshy-api-key", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"scope denied"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cmd := NewCmd(&cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"})
	cmd.SetArgs([]string{"get", "cwb/meshy-api-key"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("get should fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "forbidden") || !strings.Contains(msg, "custodian:read") || !strings.Contains(msg, "scope denied") {
		t.Fatalf("error = %q", msg)
	}
}

func TestRemoteGetNotFound(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /custodian/api/secret/cwb/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cmd := NewCmd(&cmdutil.GlobalFlags{Edge: srv.URL, Token: "tok"})
	cmd.SetArgs([]string{"get", "cwb/missing"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no such secret cwb/missing") {
		t.Fatalf("error = %v", err)
	}
}

func TestGetFormatsMissingAndDecryptFailures(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CW_SATCHEL_DIR", dir)
	restore := stubPassphrase(t, "right passphrase")
	defer restore()

	put := NewCmd(&cmdutil.GlobalFlags{})
	put.SetArgs([]string{"put", "personal/api-token"})
	put.SetIn(strings.NewReader("secret-value"))
	if err := put.Execute(); err != nil {
		t.Fatalf("put: %v", err)
	}

	restore()
	restore = stubPassphrase(t, "wrong passphrase")
	defer restore()
	get := NewCmd(&cmdutil.GlobalFlags{})
	get.SetArgs([]string{"get", "personal/api-token"})
	err := get.Execute()
	if err == nil || !strings.Contains(err.Error(), "decrypt failed for personal/api-token") {
		t.Fatalf("wrong passphrase command err = %v", err)
	}

	restore()
	restore = stubPassphrase(t, "right passphrase")
	defer restore()
	missing := NewCmd(&cmdutil.GlobalFlags{})
	missing.SetArgs([]string{"get", "personal/missing"})
	err = missing.Execute()
	if err == nil || !strings.Contains(err.Error(), "no such secret personal/missing") {
		t.Fatalf("missing command err = %v", err)
	}
}

func stubPassphrase(t *testing.T, passphrase string) func() {
	t.Helper()
	old := promptPassphrase
	promptPassphrase = func(string) (string, error) { return passphrase, nil }
	return func() { promptPassphrase = old }
}
