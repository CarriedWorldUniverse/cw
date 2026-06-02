// Package oidc talks to herald's OIDC bootstrap endpoints through the edge: it
// discovers the token/revocation endpoints and performs the password,
// jwt-bearer, and refresh_token grants plus RFC 7009 revocation. These are the
// only tokenless routes through interchange. herald is reached at <edge>/herald.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is anchored on an interchange edge URL.
type Client struct {
	edge string
	hc   *http.Client
}

// New builds a client for an edge (trailing slash trimmed).
func New(edge string) *Client {
	return &Client{edge: strings.TrimRight(edge, "/"), hc: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) heraldBase() string { return c.edge + "/herald" }

// Discovery is the subset of the OIDC discovery doc cw uses.
type Discovery struct {
	TokenEndpoint      string `json:"token_endpoint"`
	RevocationEndpoint string `json:"revocation_endpoint"`
	JWKSURI            string `json:"jwks_uri"`
}

// Discover fetches the OIDC discovery document from the edge.
func (c *Client) Discover(ctx context.Context) (Discovery, error) {
	u := c.heraldBase() + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return Discovery{}, fmt.Errorf("oidc: cannot reach herald at %s: %w", c.edge, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return Discovery{}, fmt.Errorf("oidc: discovery status %d: %s", resp.StatusCode, body)
	}
	var d Discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return Discovery{}, fmt.Errorf("oidc: discovery parse: %w", err)
	}
	if d.TokenEndpoint == "" {
		return Discovery{}, fmt.Errorf("oidc: discovery missing token_endpoint")
	}
	return d, nil
}

// Token is a grant response.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (c *Client) tokenURL(ctx context.Context) (string, error) {
	d, err := c.Discover(ctx)
	if err != nil {
		return "", err
	}
	return d.TokenEndpoint, nil
}

// PasswordGrant runs grant_type=password (human login).
func (c *Client) PasswordGrant(ctx context.Context, username, password string) (Token, error) {
	tu, err := c.tokenURL(ctx)
	if err != nil {
		return Token{}, err
	}
	return c.grant(ctx, tu, url.Values{
		"grant_type": {"password"}, "username": {username}, "password": {password},
	}, "login")
}

// JWTBearerGrant runs grant_type=jwt-bearer (agent login) with a signed assertion.
func (c *Client) JWTBearerGrant(ctx context.Context, assertion string) (Token, error) {
	tu, err := c.tokenURL(ctx)
	if err != nil {
		return Token{}, err
	}
	return c.grant(ctx, tu, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion},
	}, "assertion login")
}

// RefreshGrant runs grant_type=refresh_token.
func (c *Client) RefreshGrant(ctx context.Context, refreshToken string) (Token, error) {
	tu, err := c.tokenURL(ctx)
	if err != nil {
		return Token{}, err
	}
	return c.grant(ctx, tu, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refreshToken},
	}, "refresh")
}

// TokenEndpoint exposes the discovered token endpoint (agents need it as the
// assertion audience).
func (c *Client) TokenEndpoint(ctx context.Context) (string, error) { return c.tokenURL(ctx) }

func (c *Client) grant(ctx context.Context, tokenURL string, form url.Values, what string) (Token, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("oidc: %s: %w", what, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return Token{}, fmt.Errorf("oidc: %s rejected (status %d)", what, resp.StatusCode)
	}
	var t Token
	if err := json.Unmarshal(body, &t); err != nil {
		return Token{}, fmt.Errorf("oidc: %s decode: %w", what, err)
	}
	if t.AccessToken == "" {
		return Token{}, fmt.Errorf("oidc: %s: empty access_token", what)
	}
	return t, nil
}

// Revoke revokes a refresh token (RFC 7009; herald always answers 200).
func (c *Client) Revoke(ctx context.Context, refreshToken string) error {
	d, err := c.Discover(ctx)
	if err != nil {
		return err
	}
	ru := d.RevocationEndpoint
	if ru == "" {
		ru = c.heraldBase() + "/revoke"
	}
	form := url.Values{"token": {refreshToken}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ru, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("oidc: revoke: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("oidc: revoke status %d", resp.StatusCode)
	}
	return nil
}
