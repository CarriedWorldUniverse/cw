package auth

import (
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/spf13/cobra"
)

// newIdJAGCmd builds `cw auth idjag` — mint a single-use, audience-scoped
// ID-JAG and print it to stdout (for scripting; not stored). Unlike `login`,
// the agent's casket key signs a fresh proof-of-possession assertion each call,
// so identity comes from CW_OWNER_SEED + agent id/slug, never the stored session.
func newIdJAGCmd(gf *GlobalFlags) *cobra.Command {
	var agentID, slug, audience string
	cmd := &cobra.Command{
		Use:   "idjag",
		Short: "Mint an audience-scoped ID-JAG (agent; prints to stdout for scripting)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if audience == "" {
				return fmt.Errorf("--audience is required (the target service, e.g. ledger)")
			}
			if agentID == "" || slug == "" {
				return fmt.Errorf("idjag requires --agent-id and --slug (or CW_AGENT_ID/CW_AGENT_SLUG)")
			}
			seed := os.Getenv("CW_OWNER_SEED")
			if seed == "" {
				return fmt.Errorf("idjag requires the owner seed in CW_OWNER_SEED")
			}

			edge := gf.Edge
			name := gf.Context
			if name == "" {
				name = "default"
			}
			if edge == "" {
				if c, err := config.Load(); err == nil {
					if ctx, ok := c.Contexts[name]; ok {
						edge = ctx.Edge
					}
				}
			}
			if edge == "" {
				return fmt.Errorf("no edge: pass --edge <url> (or set it via `cw auth login`)")
			}

			ctx := cmd.Context()
			oc := oidc.New(edge)
			// The assertion's audience is the identity endpoint itself (proxy-safe,
			// matches herald's verifyAssertion); the ID-JAG's audience is --audience.
			iu, err := oc.IdentityEndpoint(ctx)
			if err != nil {
				return err
			}
			assertion, err := identity.AgentAssertion([]byte(seed), slug, agentID, iu)
			if err != nil {
				return err
			}
			tok, err := oc.IdentityAssertion(ctx, assertion, audience)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), tok.AccessToken)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentID, "agent-id", os.Getenv("CW_AGENT_ID"), "agent herald id")
	cmd.Flags().StringVar(&slug, "slug", os.Getenv("CW_AGENT_SLUG"), "agent casket key slug")
	cmd.Flags().StringVar(&audience, "audience", "", "target service the ID-JAG is scoped to (required)")
	return cmd
}
