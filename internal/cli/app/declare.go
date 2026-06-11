package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"github.com/spf13/cobra"
)

func newDeclare() *cobra.Command {
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
			conn, err := dial(envOr("CW_APP_ALMANAC_ADDR", "almanac.cwb.svc.cluster.local:8083"))
			if err != nil {
				return err
			}
			defer conn.Close()
			_, err = cwbv1.NewConfigServiceClient(conn).SetConfig(
				mdCtx(cmd.Context(), "config:write"),
				&cwbv1.SetConfigRequest{Path: declPath(name), Value: string(y)})
			if err != nil {
				return err
			}
			fmt.Printf("declared %s -> %s (mason reconciles within its poll interval; `cw app sync %s` to force)\n", name, declPath(name), name)
			return nil
		},
	}
}
