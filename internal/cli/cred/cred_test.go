package cred

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if !bytes.Contains(onDisk, []byte("casket.DeriveAgentKey")) {
		t.Fatalf("stored envelope does not describe casket derivation: %s", onDisk)
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

func TestRemoteNamespaceRecognizedButUnimplemented(t *testing.T) {
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"get", "cwb/api-token"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("remote namespace should fail in NEX-649")
	}
	if !strings.Contains(err.Error(), "remote tier not implemented yet (NEX-650)") {
		t.Fatalf("remote error = %q", err.Error())
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
