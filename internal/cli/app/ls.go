package app

import (
	"fmt"
	"text/tabwriter"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newLs(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List declared apps and their reconcile state (mason)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newAPI(gf)
			if err != nil {
				return err
			}
			apps, err := a.listApps(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tNAMESPACE\tPHASE\tREADY\tMESSAGE")
			for _, app := range apps {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					app.Name, app.Namespace, phaseDisplay(app.Phase), app.Ready, app.Message)
			}
			return w.Flush()
		},
	}
}
