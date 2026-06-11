package app

import (
	"fmt"
	"os"
	"text/tabwriter"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"github.com/spf13/cobra"
)

func masonClient() (cwbv1.AppServiceClient, func(), error) {
	conn, err := dial(envOr("CW_APP_MASON_ADDR", "mason.cwb.svc.cluster.local:8086"))
	if err != nil {
		return nil, nil, err
	}
	return cwbv1.NewAppServiceClient(conn), func() { conn.Close() }, nil
}

func phaseString(p cwbv1.AppPhase) string {
	switch p {
	case cwbv1.AppPhase_APP_PHASE_UNKNOWN:
		return "Unknown"
	case cwbv1.AppPhase_APP_PHASE_INVALID:
		return "Invalid"
	case cwbv1.AppPhase_APP_PHASE_PROGRESSING:
		return "Progressing"
	case cwbv1.AppPhase_APP_PHASE_DEGRADED:
		return "Degraded"
	case cwbv1.AppPhase_APP_PHASE_SYNCED:
		return "Synced"
	default:
		return "Unknown"
	}
}

func newLs() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List declared apps and their reconcile state (mason)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, closeConn, err := masonClient()
			if err != nil {
				return err
			}
			defer closeConn()
			resp, err := client.ListApps(mdCtx(cmd.Context(), "app:read"), &cwbv1.ListAppsRequest{})
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tNAMESPACE\tPHASE\tREADY\tMESSAGE")
			for _, a := range resp.GetApps() {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					a.GetName(), a.GetNamespace(), phaseString(a.GetPhase()), a.GetReady(), a.GetMessage())
			}
			return w.Flush()
		},
	}
}
