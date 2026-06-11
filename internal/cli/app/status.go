package app

import (
	"fmt"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"github.com/spf13/cobra"
)

func newStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status <name>",
		Short: "Show one app's reconcile state and declaration (mason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closeConn, err := masonClient()
			if err != nil {
				return err
			}
			defer closeConn()
			resp, err := client.GetApp(mdCtx(cmd.Context(), "app:read"), &cwbv1.GetAppRequest{Name: args[0]})
			if err != nil {
				return err
			}
			a := resp.GetApp()
			fmt.Printf("name:         %s\nnamespace:    %s\nphase:        %s\nready:        %s\nmessage:      %s\ndecl hash:    %s\napplied hash: %s\nlast applied: %s\nlast checked: %s\n",
				a.GetName(), a.GetNamespace(), phaseString(a.GetPhase()), a.GetReady(), a.GetMessage(),
				a.GetDeclHash(), a.GetAppliedHash(), a.GetLastAppliedAt(), a.GetLastCheckedAt())
			fmt.Println("--- declaration ---")
			fmt.Print(resp.GetDeclaration())
			return nil
		},
	}
}
