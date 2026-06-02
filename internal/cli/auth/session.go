package auth

import (
	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// session resolves the effective context and builds a client for it. With a
// static --token, it returns a token-only client (no store).
func session(gf *GlobalFlags) (*client.Client, config.Context, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Context{}, "", err
	}
	ctx, name, err := cfg.Resolve(gf.Context, gf.Edge)
	if err != nil {
		return nil, config.Context{}, "", err
	}
	if gf.Token != "" {
		return client.WithStaticToken(ctx.Edge, gf.Token), ctx, name, nil
	}
	store := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
	return client.New(ctx.Edge, store, oidc.New(ctx.Edge)), ctx, name, nil
}
