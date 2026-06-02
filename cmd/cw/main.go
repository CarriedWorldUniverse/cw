// Command cw is the CWB platform CLI for humans and agents.
package main

import (
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/cli/auth"
	pr "github.com/CarriedWorldUniverse/cw/internal/cli/pr"
	repo "github.com/CarriedWorldUniverse/cw/internal/cli/repo"
	"github.com/spf13/cobra"
)

// Global flags shared by all subcommands (precedence: flag > env > current
// context). They are bound directly onto an auth.GlobalFlags so cobra populates
// the same struct the auth subcommands read at Execute time.
var (
	flags        = &auth.GlobalFlags{}
	flagContext  = &flags.Context
	flagEdge     = &flags.Edge
	flagToken    = &flags.Token
	flagIdentity = &flags.Identity
	flagJSON     = &flags.JSON
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cw",
		Short:         "CWB platform CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	p := root.PersistentFlags()
	p.StringVar(flagContext, "context", os.Getenv("CW_CONTEXT"), "context name")
	p.StringVar(flagEdge, "edge", os.Getenv("CW_EDGE"), "interchange edge URL (override)")
	p.StringVar(flagToken, "token", os.Getenv("CW_TOKEN"), "use this bearer token directly (skip the token store)")
	p.StringVar(flagIdentity, "identity", os.Getenv("CW_IDENTITY"), "agent identity file (for --agent login)")
	p.BoolVar(flagJSON, "json", false, "machine-readable JSON output")
	root.AddCommand(auth.NewCmd(flags))
	root.AddCommand(repo.NewCmd(flags))
	root.AddCommand(pr.NewCmd(flags))
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cw: "+err.Error())
		os.Exit(1)
	}
}
