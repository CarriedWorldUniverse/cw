package credential

import (
	"os"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// TestSeamFetchGit_Live exercises the real broker seam end to end.
//
// Gated: skips unless CW_IT_SEAM_URL (broker base URL) and CW_IT_TOKEN (a
// worker broker-session JWT that has been granted a git credential) are
// set. Proves a worker fetches a scoped git credential through the seam
// with no secret in its environment — the M1 invariant (NEX-435).
//
// Run after the seam (nexus PR #236) is deployed to dMon and a git
// credential has been registered + granted to the worker identity.
func TestSeamFetchGit_Live(t *testing.T) {
	seam := os.Getenv("CW_IT_SEAM_URL")
	tok := os.Getenv("CW_IT_TOKEN")
	if seam == "" || tok == "" {
		t.Skip("set CW_IT_SEAM_URL + CW_IT_TOKEN to run the live seam proof")
	}
	t.Setenv("CW_SEAM_URL", seam)
	gf := &cmdutil.GlobalFlags{Token: tok}

	user, pass, err := seamFetchGit(gf, "github.com")
	if err != nil {
		t.Fatalf("seamFetchGit: %v", err)
	}
	if user == "" || pass == "" {
		t.Fatalf("empty credential: user=%q password-empty=%v", user, pass == "")
	}
	// NEVER log the password.
	t.Logf("seam returned username=%q (password redacted, len=%d)", user, len(pass))
}
