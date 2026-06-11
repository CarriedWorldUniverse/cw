package app

import (
	"fmt"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"github.com/spf13/cobra"
)

func newSync() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [name]",
		Short: "Trigger an immediate reconcile pass (all apps, or one)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			client, closeConn, err := masonClient()
			if err != nil {
				return err
			}
			defer closeConn()
			resp, err := client.TriggerSync(mdCtx(cmd.Context(), "app:write"), &cwbv1.TriggerSyncRequest{Name: name})
			if err != nil {
				return err
			}
			for _, a := range resp.GetApps() {
				fmt.Printf("%s: %s %s %s\n", a.GetName(), phaseString(a.GetPhase()), a.GetReady(), a.GetMessage())
			}
			return nil
		},
	}
}
