package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/CarriedWorldUniverse/cwb-client/client"
)

// edgeAPI is the default transport: the interchange edge's grpc-gateway REST
// bindings (mason.proto / almanac.proto google.api.http) with the session
// bearer. The edge verifies the token and injects cwb-* identity; mason and
// almanac enforce scopes (app:read/app:write, config:write).
type edgeAPI struct{ c client.Doer }

func (e edgeAPI) listApps(ctx context.Context) ([]appStatus, error) {
	var out struct {
		Apps []appStatus `json:"apps"`
	}
	err := e.do(ctx, http.MethodGet, "mason", "/api/apps", nil, &out)
	return out.Apps, err
}

func (e edgeAPI) getApp(ctx context.Context, name string) (appStatus, string, error) {
	var out struct {
		App         appStatus `json:"app"`
		Declaration string    `json:"declaration"`
	}
	err := e.do(ctx, http.MethodGet, "mason", "/api/apps/"+url.PathEscape(name), nil, &out)
	return out.App, out.Declaration, err
}

func (e edgeAPI) triggerSync(ctx context.Context, name string) ([]appStatus, error) {
	body := struct {
		Name string `json:"name,omitempty"`
	}{name}
	var out struct {
		Apps []appStatus `json:"apps"`
	}
	err := e.do(ctx, http.MethodPost, "mason", "/api/apps:sync", body, &out)
	return out.Apps, err
}

// declare writes the declaration via almanac SetConfig:
// PUT /api/config/{path=**} with body {"value": <yaml>}.
func (e edgeAPI) declare(ctx context.Context, name, yaml string) error {
	body := struct {
		Value string `json:"value"`
	}{yaml}
	return e.do(ctx, http.MethodPut, "almanac", configPath(name), body, nil)
}

// remove deletes the declaration via almanac DeleteConfig:
// DELETE /api/config/{path=**}.
func (e edgeAPI) remove(ctx context.Context, name string) error {
	return e.do(ctx, http.MethodDelete, "almanac", configPath(name), nil, nil)
}

// configPath builds the almanac ConfigService URL path for an app declaration.
// The prefix is intentional path structure; only the name is escaped.
func configPath(name string) string {
	return "/api/config/" + declPath(url.PathEscape(name))
}

// do marshals body, calls the pillar through the edge, maps non-2xx to a clean
// error, and decodes into out (nil out = 2xx check only). Mirrors
// cwb-client/ledger.do.
func (e edgeAPI) do(ctx context.Context, method, pillar, path string, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s: marshal: %w", pillar, err)
		}
		raw = b
	}
	resp, respBody, err := e.c.Do(ctx, method, pillar, path, raw)
	if err != nil {
		return err // includes client.ErrReauth
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s %s: %s", pillar, method, path, errMsg(respBody, resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("%s: decode %s: %w", pillar, path, err)
		}
	}
	return nil
}

// errMsg extracts the human message from a gateway error body — grpc-gateway
// emits {"code":N,"message":...}; plain edge errors may use {"error":...}.
func errMsg(body []byte, status int) string {
	var e struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	switch {
	case e.Message != "":
		return e.Message
	case e.Error != "":
		return e.Error
	default:
		return fmt.Sprintf("status %d", status)
	}
}
