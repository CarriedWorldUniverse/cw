package app

import (
	"fmt"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newSync(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "sync [name]",
		Short: "Trigger an immediate reconcile pass (all apps, or one)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			a, err := newAPI(gf)
			if err != nil {
				return err
			}
			apps, err := a.triggerSync(cmd.Context(), name)
			if err != nil {
				return err
			}
			for _, app := range apps {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s %s %s\n", app.Name, phaseDisplay(app.Phase), app.Ready, app.Message)
			}
			return nil
		},
	}
}
