package auth

import (
	"context"
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
	"github.com/spf13/cobra"
)

func newLogoutCmd(gf *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the refresh token and clear stored credentials for the context",
		RunE:  func(_ *cobra.Command, _ []string) error { return runLogout(gf) },
	}
}

func runLogout(gf *GlobalFlags) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, name, err := cfg.Resolve(gf.Context, gf.Edge)
	if err != nil {
		return err
	}
	store := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
	// Best-effort revoke at herald before wiping locally.
	if rtok, rerr := store.Refresh(); rerr == nil {
		if verr := oidc.New(ctx.Edge).Revoke(context.Background(), rtok); verr != nil {
			fmt.Fprintf(os.Stderr, "cw: revoke warning: %v\n", verr)
		}
	}
	if err := store.Clear(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Logged out of %q\n", name)
	return nil
}
