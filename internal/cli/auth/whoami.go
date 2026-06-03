package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CarriedWorldUniverse/cwb-client/herald"
	"github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/spf13/cobra"
)

// Info is the resolved identity for `whoami`: token claims merged with the
// config context (display/slug/edge are config-sourced, not in the token).
type Info struct {
	Context   string   `json:"context"`
	Edge      string   `json:"edge"`
	Kind      string   `json:"kind"`
	Subject   string   `json:"subject"`
	Display   string   `json:"display,omitempty"`
	Slug      string   `json:"slug,omitempty"`
	Org       string   `json:"org"`
	Scopes    []string `json:"scopes"`
	Products  []string `json:"products"`
	ExpiresIn int      `json:"expires_in_seconds"`
}

func whoamiInfo(gf *GlobalFlags) (Info, error) {
	c, sess, name, err := session(gf)
	if err != nil {
		return Info{}, err
	}
	tok, err := c.AccessToken(context.Background()) // ensure-fresh
	if err != nil {
		return Info{}, err
	}
	claims, err := identity.DecodeAccessClaims(tok)
	if err != nil {
		return Info{}, err
	}
	info := Info{
		Context: name,
		Edge:    sess.Edge,
		Display: sess.Identity.Display,
		Slug:    sess.Identity.Slug,
	}
	info.Subject, _ = claims["sub"].(string)
	info.Kind, _ = claims["kind"].(string)
	info.Org, _ = claims["org"].(string)
	if sc, _ := claims["scope"].(string); sc != "" {
		info.Scopes = strings.Fields(sc)
	}
	if prods, ok := claims["products"].([]any); ok {
		for _, p := range prods {
			if s, _ := p.(string); s != "" {
				info.Products = append(info.Products, s)
			}
		}
	}
	if exp, ok := claims["exp"].(float64); ok {
		info.ExpiresIn = int(time.Until(time.Unix(int64(exp), 0)).Seconds())
	}
	return info, nil
}

// NewWhoamiCmd builds the whoami command, registered both at the top level
// (`cw whoami`) and under `cw auth` (the alias). --remote fetches the
// server-authoritative record from herald's GET /api/me.
func NewWhoamiCmd(gf *GlobalFlags) *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the current identity (local; --remote for the server-authoritative record)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if remote {
				return remoteWhoami(cmd, gf)
			}
			info, err := whoamiInfo(gf)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if gf.JSON {
				return json.NewEncoder(out).Encode(info)
			}
			expires := fmt.Sprintf("%ds", info.ExpiresIn)
			if info.ExpiresIn <= 0 {
				expires = "expired"
			}
			fmt.Fprintf(out, "context:  %s\nedge:     %s\nkind:     %s\nsubject:  %s\n",
				info.Context, info.Edge, info.Kind, info.Subject)
			if info.Display != "" {
				fmt.Fprintf(out, "display:  %s\n", info.Display)
			}
			if info.Slug != "" {
				fmt.Fprintf(out, "slug:     %s\n", info.Slug)
			}
			fmt.Fprintf(out, "org:      %s\nscopes:   %s\nproducts: %s\nexpires:  %s\n",
				info.Org, strings.Join(info.Scopes, " "), strings.Join(info.Products, " "), expires)
			return nil
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "fetch the server-authoritative record from herald (GET /api/me)")
	return cmd
}

// remoteWhoami renders herald's authoritative identity record (status, org name,
// server scopes, + agent responsible_human/fingerprint).
func remoteWhoami(cmd *cobra.Command, gf *GlobalFlags) error {
	c, sess, name, err := session(gf)
	if err != nil {
		return err
	}
	ui, err := herald.Me(cmd.Context(), c)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if gf.JSON {
		return json.NewEncoder(out).Encode(ui)
	}
	fmt.Fprintf(out, "context:  %s\nedge:     %s\nid:       %s\nkind:     %s\n", name, sess.Edge, ui.ID, ui.Kind)
	if ui.DisplayName != "" {
		fmt.Fprintf(out, "display:  %s\n", ui.DisplayName)
	}
	org := ui.Org
	if ui.OrgName != "" {
		org = fmt.Sprintf("%s (%s)", ui.Org, ui.OrgName)
	}
	fmt.Fprintf(out, "org:      %s\nstatus:   %s\nscopes:   %s\n", org, ui.Status, strings.Join(ui.Scopes, " "))
	if ui.ResponsibleHuman != "" {
		fmt.Fprintf(out, "responsible_human: %s\n", ui.ResponsibleHuman)
	}
	if ui.Fingerprint != "" {
		fmt.Fprintf(out, "fingerprint:       %s\n", ui.Fingerprint)
	}
	return nil
}
