package auth

import (
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/spf13/cobra"
)

func newSwitchCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <context>",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Contexts[args[0]]; !ok {
				return fmt.Errorf("no such context %q", args[0])
			}
			cfg.CurrentContext = args[0]
			if err := cfg.Save(); err != nil {
				return err
			}
			// Confirmation to stderr — stdout stays clean for scripting.
			fmt.Fprintf(os.Stderr, "switched to %q\n", args[0])
			return nil
		},
	}
}
