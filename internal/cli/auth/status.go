package auth

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

// statusEntry is one context's freshness for `cw auth status`.
type statusEntry struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Edge    string `json:"edge"`
	Kind    string `json:"kind,omitempty"`
	Display string `json:"display,omitempty"`
	Subject string `json:"subject,omitempty"`
	Org     string `json:"org,omitempty"`
	State   string `json:"state"`
}

func newStatusCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List contexts and token freshness",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(cfg.Contexts))
			for name := range cfg.Contexts {
				names = append(names, name)
			}
			sort.Strings(names)

			entries := make([]statusEntry, 0, len(names))
			for _, name := range names {
				ctx := cfg.Contexts[name]
				// valid (cached access still live) > refreshable (refresh token
				// present) > logged-out.
				state := "logged-out"
				st := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
				if _, exp, err := st.Access(); err == nil && time.Until(exp) > 0 {
					state = "valid"
				} else if _, rerr := st.Refresh(); rerr == nil {
					state = "refreshable"
				}
				entries = append(entries, statusEntry{
					Name:    name,
					Current: name == cfg.CurrentContext,
					Edge:    ctx.Edge,
					Kind:    ctx.Identity.Kind,
					Display: ctx.Identity.Display,
					Subject: ctx.Identity.Subject,
					Org:     ctx.Identity.Org,
					State:   state,
				})
			}

			out := cmd.OutOrStdout()
			if gf.JSON {
				return json.NewEncoder(out).Encode(entries)
			}
			if len(entries) == 0 {
				fmt.Fprintln(out, "no contexts (run 'cw auth login --edge <url>')")
				return nil
			}
			for _, e := range entries {
				marker := "  "
				if e.Current {
					marker = "* "
				}
				fmt.Fprintf(out, "%s%-12s %-28s %s (%s)\n", marker, e.Name, e.Edge, e.Display, e.State)
			}
			return nil
		},
	}
}
