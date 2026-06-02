package auth

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newTokenCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print a currently-valid access token (auto-refreshing) for scripting",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, _, _, err := session(gf)
			if err != nil {
				return err
			}
			tok, err := c.AccessToken(context.Background())
			if err != nil {
				return err
			}
			fmt.Println(tok)
			return nil
		},
	}
}
