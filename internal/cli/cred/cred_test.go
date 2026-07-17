package cred

import (
	"bytes"
	"errors"
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

func TestPersonalPutRmGetDistinguishesNotFound(t *testing.T) {
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

	rm := NewCmd(&cmdutil.GlobalFlags{})
	rm.SetArgs([]string{"rm", "personal/api-token"})
	if err := rm.Execute(); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api-token.casket.json")); !os.IsNotExist(err) {
		t.Fatalf("envelope still on disk after rm: %v", err)
	}

	get := NewCmd(&cmdutil.GlobalFlags{})
	get.SetArgs([]string{"get", "personal/api-token"})
	err := get.Execute()
	if err == nil || !strings.Contains(err.Error(), "no such secret personal/api-token") {
		t.Fatalf("get after rm err = %v, want not-found", err)
	}

	rmAgain := NewCmd(&cmdutil.GlobalFlags{})
	rmAgain.SetArgs([]string{"rm", "personal/api-token"})
	err = rmAgain.Execute()
	if err == nil || !strings.Contains(err.Error(), "no such secret personal/api-token") {
		t.Fatalf("rm of missing name err = %v, want not-found", err)
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
