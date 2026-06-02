package auth

import (
	"fmt"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

func newStatusCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List contexts and token freshness",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Contexts) == 0 {
				fmt.Println("no contexts (run 'cw auth login --edge <url>')")
				return nil
			}
			for name, ctx := range cfg.Contexts {
				marker := "  "
				if name == cfg.CurrentContext {
					marker = "* "
				}
				// valid (cached access still live) > refreshable (refresh token
				// present) > logged-out.
				state := "logged-out"
				st := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
				if _, exp, err := st.Access(); err == nil && time.Until(exp) > 0 {
					state = "valid"
				} else if _, rerr := st.Refresh(); rerr == nil {
					state = "refreshable"
				}
				fmt.Printf("%s%-12s %-28s %s (%s)\n", marker, name, ctx.Edge, ctx.Identity.Display, state)
			}
			return nil
		},
	}
}
