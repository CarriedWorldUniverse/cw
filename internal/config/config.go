// Package config is cw's on-disk configuration: named contexts (each an edge
// URL + the identity logged in there) and the current context. It holds NO
// secrets — refresh tokens live in the OS keychain, access tokens in a separate
// cache, agent seeds in env/keychain. File: $CW_CONFIG_DIR (or
// $XDG_CONFIG_HOME/cw, or ~/.config/cw)/config.yaml, mode 0600.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Identity is who is logged in to a context. Subject/Display are filled from the
// access token after login; Slug is set for agents (the casket key slug).
type Identity struct {
	Kind    string `yaml:"kind"`              // "human" | "agent"
	Subject string `yaml:"subject,omitempty"` // herald user id
	Display string `yaml:"display,omitempty"` // email / display name
	Slug    string `yaml:"slug,omitempty"`    // agent only
	Org     string `yaml:"org,omitempty"`     // herald org id (from the token's org claim)
}

// Context binds an edge URL to an identity.
type Context struct {
	Edge     string   `yaml:"edge"`
	Identity Identity `yaml:"identity"`
}

// Config is the whole file.
type Config struct {
	CurrentContext string             `yaml:"current-context"`
	Contexts       map[string]Context `yaml:"contexts"`
}

// Dir returns cw's config directory, honouring CW_CONFIG_DIR, then
// XDG_CONFIG_HOME/cw, then ~/.config/cw.
func Dir() string {
	if d := os.Getenv("CW_CONFIG_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "cw")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cw")
}

func path() string { return filepath.Join(Dir(), "config.yaml") }

// Load reads the config. A missing file yields an empty (usable) Config.
func Load() (*Config, error) {
	b, err := os.ReadFile(path())
	if errors.Is(err, os.ErrNotExist) {
		return &Config{Contexts: map[string]Context{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	if c.Contexts == nil {
		c.Contexts = map[string]Context{}
	}
	return &c, nil
}

// Save writes the config 0600, creating the directory if needed. The write is
// atomic (temp file in the same dir + rename) so a crash or a racing writer
// (e.g. parallel agent invocations) can never leave a truncated config.
func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(Dir(), ".config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("config: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: chmod: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close: %w", err)
	}
	if err := os.Rename(tmpName, path()); err != nil {
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

// Upsert adds or replaces a context.
func (c *Config) Upsert(name string, ctx Context) {
	if c.Contexts == nil {
		c.Contexts = map[string]Context{}
	}
	c.Contexts[name] = ctx
}

// Current returns the current context (false if unset/missing).
func (c *Config) Current() (Context, bool) {
	ctx, ok := c.Contexts[c.CurrentContext]
	return ctx, ok
}

// Resolve picks the effective context from flag overrides:
//   - name set  -> that named context (error if unknown; edge override applied)
//   - edge set  -> the current context with Edge replaced. If there is no
//     current context this yields an edge-only context with an EMPTY identity —
//     valid for the --token path (caller supplies a bearer) and for first-run
//     `login` (which builds its own context); callers needing a stored identity
//     must check ctx.Identity.
//   - neither   -> the current context (error if none)
func (c *Config) Resolve(name, edge string) (Context, string, error) {
	if name != "" {
		ctx, ok := c.Contexts[name]
		if !ok {
			return Context{}, "", fmt.Errorf("config: no such context %q", name)
		}
		if edge != "" {
			ctx.Edge = edge
		}
		return ctx, name, nil
	}
	if edge != "" {
		ctx, _ := c.Current()
		ctx.Edge = edge
		return ctx, c.CurrentContext, nil
	}
	ctx, ok := c.Current()
	if !ok {
		return Context{}, "", errors.New("config: no current context (run 'cw auth login --edge <url>')")
	}
	return ctx, c.CurrentContext, nil
}
