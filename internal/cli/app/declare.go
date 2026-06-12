package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newDeclare(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "declare <file.yaml>",
		Short: "Write an app declaration to almanac (mason reconciles it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			y, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			name := strings.TrimSuffix(filepath.Base(args[0]), filepath.Ext(args[0]))
			name = strings.TrimSuffix(name, ".values") // lynxai.values.yaml -> lynxai
			if err := precheck(name, y); err != nil {
				return err
			}
			a, err := newAPI(gf)
			if err != nil {
				return err
			}
			if err := a.declare(cmd.Context(), name, string(y)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "declared %s -> %s (mason reconciles within its poll interval; `cw app sync %s` to force)\n", name, declPath(name), name)
			return nil
		},
	}
}
