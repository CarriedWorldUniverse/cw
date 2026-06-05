// Package credential implements `cw credential`: the git credential
// helper backed by the custodian seam (NEX-435). A k3s worker registers
// `cw credential git-helper` as git's credential.helper so `git push`
// authenticates via the broker seam without the agent ever holding the PAT.
package credential

import (
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmd builds the `cw credential` command group.
func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "credential", Short: "Custodian-brokered credentials"}
	cmd.AddCommand(newGitHelperCmd(gf))
	return cmd
}

func newGitHelperCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "git-helper [get|store|erase]",
		Short:         "git credential helper backed by the custodian seam",
		Args:          cobra.ExactArgs(1),
		Hidden:        true, // invoked by git, not humans
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fetch := func(host string) (string, string, error) {
				return seamFetchGit(gf, host)
			}
			return runGitHelper(args[0], os.Stdin, os.Stdout, fetch)
		},
	}
}
