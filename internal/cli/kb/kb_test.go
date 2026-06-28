package kb

import (
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestStoreContentFromReader checks the content sourcing helper: --content wins,
// else the provided reader (stdin stand-in).
func TestStoreContentFromReader(t *testing.T) {
	got, err := readContent("flag-content", strings.NewReader("ignored"))
	if err != nil || got != "flag-content" {
		t.Fatalf("flag path: %q %v", got, err)
	}
	got, err = readContent("", strings.NewReader("piped body"))
	if err != nil || got != "piped body" {
		t.Fatalf("reader path: %q %v", got, err)
	}
	if _, err := readContent("", strings.NewReader("")); err == nil {
		t.Fatal("empty flag + empty reader should error")
	}
}

// The CLI must validate input BEFORE dialing commonplace, so these guard checks
// surface a clear usage error rather than a transport failure. We run them with
// no CW_APP_TLS_* set; the expected errors fire before any dial.

func TestStoreRequiresTopic(t *testing.T) {
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"store", "--content", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("store without --topic should error before dialing")
	}
}

func TestKbUpdateNothing(t *testing.T) {
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"update", "e1"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("update with no flags should error before dialing; got %v", err)
	}
}

func TestKbDeleteRequiresYes(t *testing.T) {
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"delete", "e1"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("delete without --yes should error before dialing; got %v", err)
	}
}

// TestNeedsMeshCert: with a valid request shape but no mesh cert, the command
// fails with the in-mesh-transport guidance rather than a confusing dial error.
func TestNeedsMeshCert(t *testing.T) {
	for _, k := range []string{"CW_APP_TLS_CERT", "CW_APP_TLS_KEY", "CW_APP_TLS_CA"} {
		t.Setenv(k, "")
	}
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "in-mesh mTLS") {
		t.Fatalf("list without mesh cert should give the mTLS-transport hint; got %v", err)
	}
}
