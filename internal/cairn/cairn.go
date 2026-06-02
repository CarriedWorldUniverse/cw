// Package cairn wraps cairn's REST surface (through the edge gateway) as typed
// Go over a *client.Client. The "cairn" pillar prefix + bearer + silent refresh
// are the client's job; this package owns paths, JSON shapes, and error mapping.
package cairn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

// Repo / Pull / MergeResult mirror cairn's response_body-flattened JSON.
type Repo struct {
	ID            string `json:"id"`
	Org           string `json:"org"`
	Slug          string `json:"slug"`
	DefaultBranch string `json:"default_branch"`
}
type Pull struct {
	ID             string `json:"id"`
	Repo           string `json:"repo"`
	Source         string `json:"source"`
	Target         string `json:"target"`
	Title          string `json:"title"`
	State          string `json:"state"`
	LedgerIssueKey string `json:"ledger_issue_key"`
	URL            string `json:"url"`
}
type MergeResult struct {
	ID                 string `json:"id"`
	State              string `json:"state"`
	Target             string `json:"target"`
	MergedSHA          string `json:"merged_sha"`
	LedgerCommentError string `json:"ledger_comment_error"`
}

// OpenPullInput is the create-PR body (project = ledger project key; Description
// + DefinitionOfDone optional).
type OpenPullInput struct {
	Source           string `json:"source"`
	Target           string `json:"target"`
	Title            string `json:"title"`
	Description      string `json:"description,omitempty"`
	Project          string `json:"project"`
	DefinitionOfDone string `json:"definition_of_done,omitempty"`
}

func reposPath(org string) string       { return "/api/orgs/" + org + "/repos" }
func pullsPath(org, slug string) string { return reposPath(org) + "/" + slug + "/pulls" }

// do is the shared request/decode/error-map helper.
func do(ctx context.Context, c *client.Client, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cairn: marshal: %w", err)
		}
		raw = b
	}
	resp, respBody, err := c.Do(ctx, method, "cairn", path, raw)
	if err != nil {
		return err // includes client.ErrReauth
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("cairn: %s %s: %s", method, path, errMsg(respBody, resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("cairn: decode %s: %w", path, err)
		}
	}
	return nil
}

// errMsg extracts cairn's {"error":"..."} / {"message":"..."} or falls back to status.
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

func CreateRepo(ctx context.Context, c *client.Client, org, slug string) (Repo, error) {
	var r Repo
	err := do(ctx, c, http.MethodPost, reposPath(org), map[string]string{"slug": slug}, &r)
	return r, err
}

func ListRepos(ctx context.Context, c *client.Client, org string) ([]Repo, error) {
	var rs []Repo
	err := do(ctx, c, http.MethodGet, reposPath(org), nil, &rs)
	return rs, err
}

func OpenPull(ctx context.Context, c *client.Client, org, slug string, in OpenPullInput) (Pull, error) {
	var p Pull
	err := do(ctx, c, http.MethodPost, pullsPath(org, slug), in, &p)
	return p, err
}

func ListPulls(ctx context.Context, c *client.Client, org, slug, state string) ([]Pull, error) {
	path := pullsPath(org, slug)
	if state != "" {
		path += "?state=" + url.QueryEscape(state)
	}
	var ps []Pull
	err := do(ctx, c, http.MethodGet, path, nil, &ps)
	return ps, err
}

func GetPull(ctx context.Context, c *client.Client, org, slug, id string) (Pull, error) {
	var p Pull
	err := do(ctx, c, http.MethodGet, pullsPath(org, slug)+"/"+id, nil, &p)
	return p, err
}

func MergePull(ctx context.Context, c *client.Client, org, slug, id string) (MergeResult, error) {
	var r MergeResult
	err := do(ctx, c, http.MethodPost, pullsPath(org, slug)+"/"+id+"/merge", nil, &r)
	return r, err
}
