// Package commonplace wraps commonplace's knowledge REST surface (through the
// edge gateway) as typed Go over a *client.Client. Routes are token-org-scoped
// (no {org}); owner is server-derived, never sent. Mirrors internal/cairn.
package commonplace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/CarriedWorldUniverse/cw/internal/client"
)

type Entry struct {
	ID         string   `json:"id"`
	Org        string   `json:"org"`
	Owner      string   `json:"owner"`
	Topic      string   `json:"topic"`
	Content    string   `json:"content"`
	Visibility string   `json:"visibility"`
	Tags       []string `json:"tags"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}
type Hit struct {
	Entry Entry   `json:"entry"`
	Score float64 `json:"score"`
}
type StoreInput struct {
	Topic      string   `json:"topic"`
	Content    string   `json:"content"`
	Visibility string   `json:"visibility,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

const base = "/api/knowledge"

// do marshals body, calls the knowledge pillar, maps non-2xx to an error,
// decodes into out (nil out = 2xx check only). Mirrors internal/cairn.do.
func do(ctx context.Context, c *client.Client, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("commonplace: marshal: %w", err)
		}
		raw = b
	}
	resp, respBody, err := c.Do(ctx, method, "knowledge", path, raw)
	if err != nil {
		return err // includes client.ErrReauth
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("commonplace: %s %s: %s", method, path, errMsg(respBody, resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("commonplace: decode %s: %w", path, err)
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

func Store(ctx context.Context, c *client.Client, in StoreInput) (Entry, error) {
	var e Entry
	err := do(ctx, c, http.MethodPost, base, in, &e)
	return e, err
}

func Search(ctx context.Context, c *client.Client, q string, topK int) ([]Hit, error) {
	qs := url.Values{"q": {q}, "top_k": {strconv.Itoa(topK)}}.Encode()
	var w struct {
		Hits []Hit `json:"hits"`
	}
	err := do(ctx, c, http.MethodGet, base+"/search?"+qs, nil, &w)
	return w.Hits, err
}

func List(ctx context.Context, c *client.Client) ([]Entry, error) {
	var w struct {
		Entries []Entry `json:"entries"`
	}
	err := do(ctx, c, http.MethodGet, base, nil, &w)
	return w.Entries, err
}
