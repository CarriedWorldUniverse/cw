package app

import (
	"fmt"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"github.com/spf13/cobra"
)

func newRm() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove an app declaration from almanac (mason prunes it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := dial(envOr("CW_APP_ALMANAC_ADDR", "almanac.cwb.svc.cluster.local:8083"))
			if err != nil {
				return err
			}
			defer conn.Close()
			_, err = cwbv1.NewConfigServiceClient(conn).DeleteConfig(
				mdCtx(cmd.Context(), "config:write"),
				&cwbv1.DeleteConfigRequest{Path: declPath(args[0])})
			if err != nil {
				return err
			}
			fmt.Printf("removed %s — mason prunes on its next pass\n", declPath(args[0]))
			return nil
		},
	}
}
