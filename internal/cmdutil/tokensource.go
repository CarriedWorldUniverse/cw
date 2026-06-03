package cmdutil

import (
	"context"
	"time"

	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cwb-client/oidc"

	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

const skew = 60 * time.Second

// storeSource is cw's tokenstore+oidc-backed TokenSource (silent refresh) — the
// refresh policy relocated out of the client seam.
type storeSource struct {
	store *tokenstore.Store
	oc    *oidc.Client
}

func (s *storeSource) Token(ctx context.Context) (string, error) {
	tok, exp, err := s.store.Access()
	if err == nil && time.Until(exp) > skew {
		return tok, nil
	}
	return s.Refresh(ctx)
}

func (s *storeSource) Refresh(ctx context.Context) (string, error) {
	rtok, err := s.store.Refresh()
	if err != nil {
		return "", client.ErrReauth
	}
	t, err := s.oc.RefreshGrant(ctx, rtok)
	if err != nil {
		return "", client.ErrReauth
	}
	if err := s.store.SaveRefresh(t.RefreshToken); err != nil {
		return "", err
	}
	if err := s.store.SaveAccess(t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second)); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}
