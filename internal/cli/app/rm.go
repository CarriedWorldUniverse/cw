package app

import (
	"fmt"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newRm(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove an app declaration from almanac (mason prunes it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newAPI(gf)
			if err != nil {
				return err
			}
			if err := a.remove(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s — mason prunes on its next pass\n", declPath(args[0]))
			return nil
		},
	}
}
