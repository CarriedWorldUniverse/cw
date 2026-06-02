// Package client is the edge-anchored HTTP seam every cw command group uses. It
// keeps the access token fresh (silently running the refresh_token grant before
// expiry, and once more on a 401) and makes authenticated calls to product
// routes under the edge (<edge>/<pillar>/<path>). It needs no knowledge of which
// pillar — callers name the pillar prefix.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// skew refreshes a little before the real expiry to avoid races.
const skew = 60 * time.Second

// ErrReauth means the refresh token is gone/expired/revoked — the caller must
// `cw auth login` again (or, for an agent, re-mint from its seed).
var ErrReauth = errors.New("session expired: run 'cw auth login'")

// Client targets one edge as one identity.
type Client struct {
	edge  string
	store *tokenstore.Store
	oidc  *oidc.Client
	hc    *http.Client
	// staticToken bypasses the store entirely (the --token / CW_TOKEN path).
	staticToken string
}

// New builds a Client from an edge + token store + oidc client.
func New(edge string, store *tokenstore.Store, oc *oidc.Client) *Client {
	return &Client{
		edge: strings.TrimRight(edge, "/"), store: store, oidc: oc,
		hc: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithStaticToken returns a Client that always uses the given bearer and never
// touches the token store (stateless per-invocation use, e.g. ToolRunner agents).
func WithStaticToken(edge, token string) *Client {
	return &Client{edge: strings.TrimRight(edge, "/"), staticToken: token, hc: &http.Client{Timeout: 30 * time.Second}}
}

// bearer returns a currently-valid access token, silently refreshing if the
// cached one is within skew of expiry. Returns ErrReauth when no path to a fresh
// token exists.
func (c *Client) bearer(ctx context.Context) (string, error) {
	if c.staticToken != "" {
		return c.staticToken, nil
	}
	tok, exp, err := c.store.Access()
	if err == nil && time.Until(exp) > skew {
		return tok, nil
	}
	return c.refresh(ctx)
}

// AccessToken returns a currently-valid access token (silently refreshing). It
// is the public form of bearer, used by `cw auth whoami`/`token`.
func (c *Client) AccessToken(ctx context.Context) (string, error) { return c.bearer(ctx) }

func (c *Client) refresh(ctx context.Context) (string, error) {
	rtok, err := c.store.Refresh()
	if err != nil {
		return "", ErrReauth
	}
	t, err := c.oidc.RefreshGrant(ctx, rtok)
	if err != nil {
		return "", ErrReauth
	}
	if err := c.store.SaveRefresh(t.RefreshToken); err != nil {
		return "", err
	}
	exp := time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	if err := c.store.SaveAccess(t.AccessToken, exp); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

// URL builds a product URL: <edge>/<pillar><path> (path begins with "/").
func (c *Client) URL(pillar, path string) string {
	return c.edge + "/" + strings.Trim(pillar, "/") + path
}

// Do executes an authenticated request, injecting the bearer and retrying once
// after a silent refresh on a 401. body is the full request body (may be nil);
// it is taken as []byte rather than an io.Reader so the 401-retry can safely
// resend it (a streamed reader would already be drained on the second attempt).
func (c *Client) Do(ctx context.Context, method, pillar, path string, body []byte) (*http.Response, []byte, error) {
	tok, err := c.bearer(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, raw, err := c.do(ctx, method, c.URL(pillar, path), tok, body)
	if err != nil {
		return nil, nil, err
	}
	// One refresh-and-retry on 401 (token may have been revoked server-side). If
	// the refresh itself fails, propagate that error (ErrReauth) rather than
	// returning the bare 401 — so callers get the same "run cw auth login" signal
	// the proactive path gives, not an unexplained 401.
	if resp.StatusCode == http.StatusUnauthorized && c.staticToken == "" {
		fresh, rerr := c.refresh(ctx)
		if rerr != nil {
			return nil, nil, rerr
		}
		return c.do(ctx, method, c.URL(pillar, path), fresh, body)
	}
	return resp, raw, nil
}

// Get is a convenience wrapper.
func (c *Client) Get(ctx context.Context, pillar, path string) (*http.Response, []byte, error) {
	return c.Do(ctx, http.MethodGet, pillar, path, nil)
}

func (c *Client) do(ctx context.Context, method, url, bearer string, body []byte) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err == nil && body != nil {
		req.Header.Set("Content-Type", "application/json") // cw product bodies are JSON
	}
	if err != nil {
		return nil, nil, fmt.Errorf("client: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("client: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("client: %s %s: read body: %w", method, url, err)
	}
	return resp, raw, nil
}
