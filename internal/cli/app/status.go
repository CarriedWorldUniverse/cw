package app

import (
	"fmt"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newStatus(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status <name>",
		Short: "Show one app's reconcile state and declaration (mason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newAPI(gf)
			if err != nil {
				return err
			}
			app, decl, err := a.getApp(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "name:         %s\nnamespace:    %s\nphase:        %s\nready:        %s\nmessage:      %s\ndecl hash:    %s\napplied hash: %s\nlast applied: %s\nlast checked: %s\n",
				app.Name, app.Namespace, phaseDisplay(app.Phase), app.Ready, app.Message,
				app.DeclHash, app.AppliedHash, app.LastApplied, app.LastChecked)
			fmt.Fprintln(out, "--- declaration ---")
			fmt.Fprint(out, decl)
			return nil
		},
	}
}
