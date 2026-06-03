// Package herald wraps herald's AdminService REST surface (org + identity
// provisioning, through the edge /herald prefix) as typed Go over a
// *client.Client. Mirrors internal/cairn. Path-param values live only in the
// path (url.PathEscape'd); request bodies carry only the body:"*" fields.
package herald

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Human struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Org         string `json:"org"`
}
type CreateOrgInput struct {
	Name     string   `json:"name"`
	Products []string `json:"products,omitempty"`
}
type CreateHumanInput struct {
	DisplayName string   `json:"display_name"`
	Scopes      []string `json:"scopes,omitempty"`
}
type Agent struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	DisplayName      string   `json:"display_name"`
	Org              string   `json:"org"`
	ResponsibleHuman string   `json:"responsible_human"`
	Fingerprint      string   `json:"fingerprint"`
	Status           string   `json:"status"`
	Active           bool     `json:"active"`
	Scopes           []string `json:"scopes"`
}
type CreateAgentInput struct {
	DisplayName      string   `json:"display_name"`
	ResponsibleHuman string   `json:"responsible_human"`
	CasketPubkey     string   `json:"casket_pubkey"`
	Scopes           []string `json:"scopes,omitempty"`
}
type DeleteResult struct {
	Deleted string   `json:"deleted"`
	Pillars []string `json:"pillars"`
}

// do marshals body, calls the herald pillar, maps non-2xx to an error, decodes
// into out (nil out = 2xx check only). Mirrors internal/cairn.do.
func do(ctx context.Context, c *client.Client, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("herald: marshal: %w", err)
		}
		raw = b
	}
	resp, respBody, err := c.Do(ctx, method, "herald", path, raw)
	if err != nil {
		return err // includes client.ErrReauth
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("herald: %s %s: %s", method, path, errMsg(respBody, resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("herald: decode %s: %w", path, err)
		}
	}
	return nil
}

func errMsg(body []byte, status int) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	switch {
	case e.Error != "":
		return e.Error
	case e.Message != "":
		return e.Message
	default:
		return fmt.Sprintf("status %d", status)
	}
}

func CreateOrg(ctx context.Context, c *client.Client, in CreateOrgInput) (Org, error) {
	var o Org
	err := do(ctx, c, http.MethodPost, "/api/orgs", in, &o)
	return o, err
}

func ListOrgs(ctx context.Context, c *client.Client) ([]Org, error) {
	var w struct {
		Orgs []Org `json:"orgs"`
	}
	err := do(ctx, c, http.MethodGet, "/api/orgs", nil, &w)
	return w.Orgs, err
}

func DeleteOrg(ctx context.Context, c *client.Client, id, name string) (DeleteResult, error) {
	var r DeleteResult
	path := "/api/orgs/" + url.PathEscape(id)
	err := do(ctx, c, http.MethodDelete, path, map[string]string{"name": name}, &r)
	return r, err
}

func GetProducts(ctx context.Context, c *client.Client, org string) (map[string]bool, error) {
	out := map[string]bool{}
	path := "/api/orgs/" + url.PathEscape(org) + "/products"
	err := do(ctx, c, http.MethodGet, path, nil, &out)
	return out, err
}

func EnableProduct(ctx context.Context, c *client.Client, org, product string) (map[string]bool, error) {
	out := map[string]bool{}
	path := "/api/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/enable"
	err := do(ctx, c, http.MethodPost, path, nil, &out)
	return out, err
}

func DisableProduct(ctx context.Context, c *client.Client, org, product string) (map[string]bool, error) {
	out := map[string]bool{}
	path := "/api/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/disable"
	err := do(ctx, c, http.MethodPost, path, nil, &out)
	return out, err
}

func CreateHuman(ctx context.Context, c *client.Client, org string, in CreateHumanInput) (Human, error) {
	var h Human
	path := "/api/orgs/" + url.PathEscape(org) + "/humans"
	err := do(ctx, c, http.MethodPost, path, in, &h)
	return h, err
}

func CreateAgent(ctx context.Context, c *client.Client, org string, in CreateAgentInput) (Agent, error) {
	var a Agent
	path := "/api/orgs/" + url.PathEscape(org) + "/agents"
	err := do(ctx, c, http.MethodPost, path, in, &a)
	return a, err
}

func SetHumanPassword(ctx context.Context, c *client.Client, id, password string) error {
	path := "/api/humans/" + url.PathEscape(id) + "/password"
	return do(ctx, c, http.MethodPost, path, map[string]string{"password": password}, nil)
}

// UserInfo is the caller's own authoritative identity from GET /api/me (agent
// fields empty for humans).
type UserInfo struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	DisplayName      string   `json:"display_name"`
	Org              string   `json:"org"`
	OrgName          string   `json:"org_name"`
	Status           string   `json:"status"`
	Scopes           []string `json:"scopes"`
	ResponsibleHuman string   `json:"responsible_human"`
	Fingerprint      string   `json:"fingerprint"`
}

// Me returns the caller's own authoritative identity record (server-side).
func Me(ctx context.Context, c *client.Client) (UserInfo, error) {
	var ui UserInfo
	err := do(ctx, c, http.MethodGet, "/api/me", nil, &ui)
	return ui, err
}
