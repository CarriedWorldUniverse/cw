// Package ledger wraps ledger's REST surface (through the edge gateway) as typed
// Go over a *client.Client. Issue routes are token-org-scoped (no {org} in the
// path); actor/reporter are server-derived and never sent. Mirrors internal/cairn.
package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

type Issue struct {
	Key              string `json:"key"`
	Project          string `json:"project"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Summary          string `json:"summary"`
	Description      string `json:"description"`
	DefinitionOfDone string `json:"definition_of_done"`
	Priority         string `json:"priority"`
	AssigneeAspect   string `json:"assignee_aspect"`
	Reporter         string `json:"reporter"`
	ParentKey        string `json:"parent_key"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}
type IssueRef struct {
	Key            string `json:"key"`
	Project        string `json:"project"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	Summary        string `json:"summary"`
	Priority       string `json:"priority"`
	AssigneeAspect string `json:"assignee_aspect"`
	UpdatedAt      string `json:"updated_at"`
}
type CreateInput struct {
	Project          string `json:"project"`
	Type             string `json:"type"`
	Summary          string `json:"summary"`
	Description      string `json:"description,omitempty"`
	DefinitionOfDone string `json:"definition_of_done,omitempty"`
	Priority         string `json:"priority,omitempty"`
}

const base = "/api/issues"

func keyPath(key string) string { return base + "/" + url.PathEscape(key) }

// do marshals body, calls the ledger pillar, maps non-2xx to an error, decodes
// into out (nil out = 2xx check only). Mirrors internal/cairn.do.
func do(ctx context.Context, c *client.Client, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("ledger: marshal: %w", err)
		}
		raw = b
	}
	resp, respBody, err := c.Do(ctx, method, "ledger", path, raw)
	if err != nil {
		return err // includes client.ErrReauth
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ledger: %s %s: %s", method, path, errMsg(respBody, resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("ledger: decode %s: %w", path, err)
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

func CreateIssue(ctx context.Context, c *client.Client, in CreateInput) (Issue, error) {
	var iss Issue
	err := do(ctx, c, http.MethodPost, base, in, &iss)
	return iss, err
}

func GetIssue(ctx context.Context, c *client.Client, key string) (Issue, error) {
	var iss Issue
	err := do(ctx, c, http.MethodGet, keyPath(key), nil, &iss)
	return iss, err
}

func ListMine(ctx context.Context, c *client.Client) ([]IssueRef, error) {
	return listRefs(ctx, c, base+"/my", "issues")
}
func ListReady(ctx context.Context, c *client.Client) ([]IssueRef, error) {
	return listRefs(ctx, c, base+"/ready", "issues")
}

// SearchByProject filters issues to one project key.
func SearchByProject(ctx context.Context, c *client.Client, project string) ([]IssueRef, error) {
	body := map[string]any{"filter": map[string]any{"projects": []string{project}}}
	var w struct {
		Refs []IssueRef `json:"refs"`
	}
	err := do(ctx, c, http.MethodPost, base+"/search", body, &w)
	return w.Refs, err
}

// listRefs decodes the wrapped {<field>:[IssueRef]} list responses.
func listRefs(ctx context.Context, c *client.Client, path, field string) ([]IssueRef, error) {
	var w map[string][]IssueRef
	if err := do(ctx, c, http.MethodGet, path, nil, &w); err != nil {
		return nil, err
	}
	return w[field], nil
}

func Claim(ctx context.Context, c *client.Client, key string) (Issue, error) {
	var iss Issue
	err := do(ctx, c, http.MethodPost, keyPath(key)+"/claim", nil, &iss)
	return iss, err
}

func Transition(ctx context.Context, c *client.Client, key, status string) error {
	return do(ctx, c, http.MethodPost, keyPath(key)+"/transition", map[string]string{"status": status}, nil)
}

func Comment(ctx context.Context, c *client.Client, key, body string) error {
	return do(ctx, c, http.MethodPost, keyPath(key)+"/comments", map[string]string{"body": body}, nil)
}
