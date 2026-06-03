package auth

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/prompt"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

type loginOpts struct {
	edge        string
	contextName string
	agent       bool
	// human (injected in tests; otherwise prompted)
	username, password string
	// agent
	agentID, slug string
	seed          []byte
}

func newLoginCmd(gf *GlobalFlags) *cobra.Command {
	var agent bool
	var agentID, slug string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in as a human (password) or agent (--agent, casket assertion)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name := gf.Context
			if name == "" {
				name = "default"
			}
			opts := loginOpts{edge: gf.Edge, contextName: name, agent: agent, agentID: agentID, slug: slug}
			if opts.edge == "" {
				// Fall back to the named context's existing edge.
				if c, err := config.Load(); err == nil {
					if ctx, ok := c.Contexts[name]; ok {
						opts.edge = ctx.Edge
					}
				}
			}
			if opts.edge == "" {
				return fmt.Errorf("no edge: pass --edge <url> on first login")
			}
			if agent {
				if err := loadAgentIdentity(&opts); err != nil {
					return err
				}
			} else {
				u, p, err := prompt.PromptHuman(os.Stdin)
				if err != nil {
					return err
				}
				opts.username, opts.password = u, p
			}
			return runLogin(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&agent, "agent", false, "log in as an agent (casket assertion)")
	cmd.Flags().StringVar(&agentID, "agent-id", os.Getenv("CW_AGENT_ID"), "agent herald id")
	cmd.Flags().StringVar(&slug, "slug", os.Getenv("CW_AGENT_SLUG"), "agent casket key slug")
	return cmd
}

// loadAgentIdentity fills agentID/slug/seed from flags/env (the identity-file
// source is a later refinement; env + flags cover the ToolRunner path now).
func loadAgentIdentity(o *loginOpts) error {
	if o.agentID == "" || o.slug == "" {
		return fmt.Errorf("--agent requires --agent-id and --slug (or CW_AGENT_ID/CW_AGENT_SLUG)")
	}
	seed := os.Getenv("CW_OWNER_SEED")
	if seed == "" {
		return fmt.Errorf("--agent requires the owner seed in CW_OWNER_SEED")
	}
	o.seed = []byte(seed)
	return nil
}

// runLogin performs the grant, stores the tokens, and writes back the context +
// identity (decoded from the access token). Credentials/seed come pre-filled on
// opts (prompted or flag/env sourced by the caller).
func runLogin(ctx context.Context, o loginOpts) error {
	oc := oidc.New(o.edge)
	var tok oidc.Token
	var err error
	if o.agent {
		// TokenEndpoint here is for the assertion audience; JWTBearerGrant calls
		// it again internally (two discovery round-trips on agent login — fine at
		// human-paced login, no caching needed).
		var tu string
		tu, err = oc.TokenEndpoint(ctx)
		if err != nil {
			return err
		}
		var assertion string
		assertion, err = identity.AgentAssertion(o.seed, o.slug, o.agentID, tu)
		if err != nil {
			return err
		}
		tok, err = oc.JWTBearerGrant(ctx, assertion)
	} else {
		tok, err = oc.PasswordGrant(ctx, o.username, o.password)
	}
	if err != nil {
		return err
	}

	// Claims are unverified (display + keychain-key only); type-asserts yield
	// zero values on a non-JWT/opaque token, so fall back below.
	claims, _ := identity.DecodeAccessClaims(tok.AccessToken)
	subject, _ := claims["sub"].(string)
	kind, _ := claims["kind"].(string)
	if kind == "" {
		if o.agent {
			kind = "agent"
		} else {
			kind = "human"
		}
	}
	display := o.username
	if o.agent {
		display = o.slug
	}
	// subject is the keychain-key discriminant (multi-account on one edge); never
	// leave it blank if herald returned an opaque/non-JWT token.
	if subject == "" {
		subject = display
	}

	store := tokenstore.New(o.edge, o.contextName, subject)
	if err := store.SaveRefresh(tok.RefreshToken); err != nil {
		return err
	}
	exp := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if err := store.SaveAccess(tok.AccessToken, exp); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	org, _ := claims["org"].(string)
	cfg.Upsert(o.contextName, config.Context{
		Edge:     o.edge,
		Identity: config.Identity{Kind: kind, Subject: subject, Display: display, Slug: o.slug, Org: org},
	})
	cfg.CurrentContext = o.contextName
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Logged in to %q as %s (%s)\n", o.contextName, display, kind)
	return nil
}
