package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/identity"
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

func newWhoamiCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the current identity (subject, org, scopes, products)",
		RunE: func(_ *cobra.Command, _ []string) error {
			info, err := whoamiInfo(gf)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(info)
			}
			expires := fmt.Sprintf("%ds", info.ExpiresIn)
			if info.ExpiresIn <= 0 {
				expires = "expired"
			}
			fmt.Printf("context:  %s\nedge:     %s\nkind:     %s\nsubject:  %s\n",
				info.Context, info.Edge, info.Kind, info.Subject)
			if info.Display != "" {
				fmt.Printf("display:  %s\n", info.Display)
			}
			if info.Slug != "" {
				fmt.Printf("slug:     %s\n", info.Slug)
			}
			fmt.Printf("org:      %s\nscopes:   %s\nproducts: %s\nexpires:  %s\n",
				info.Org, strings.Join(info.Scopes, " "), strings.Join(info.Products, " "), expires)
			return nil
		},
	}
}
