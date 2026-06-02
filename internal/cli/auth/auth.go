// Package auth implements the `cw auth` command group: login, logout, whoami,
// status, switch, token. It wires config + tokenstore + oidc + identity.
//
// Output convention (followed by all command groups): query commands (whoami,
// status, token) write their result to STDOUT so it can be piped/parsed;
// side-effect commands (login, logout, switch) write confirmations to STDERR so
// stdout stays clean; `--json` always goes to stdout.
package auth

import "github.com/spf13/cobra"

// GlobalFlags carries the root persistent flags the auth commands read.
type GlobalFlags struct {
	Context  string
	Edge     string
	Token    string
	Identity string
	JSON     bool
}

// NewCmd builds the `cw auth` command tree. gf points at the root's flag vars,
// which cobra populates at Execute time; subcommands read gf inside RunE.
func NewCmd(gf *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Authenticate to the CWB platform (herald)"}
	cmd.AddCommand(
		newLoginCmd(gf),
		newLogoutCmd(gf),
		newWhoamiCmd(gf),
		newStatusCmd(gf),
		newSwitchCmd(gf),
		newTokenCmd(gf),
	)
	return cmd
}
