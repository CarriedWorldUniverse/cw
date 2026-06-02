// Package tokenstore persists a context's tokens: the durable refresh token in
// the OS keychain (go-keyring; macOS Keychain / Linux Secret Service, encrypted
// file fallback) and the short-lived access token (+ its expiry) in a 0600
// cache file under the config dir. Keyed by {edge, subject} so multiple
// identities/edges don't collide.
package tokenstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/zalando/go-keyring"
)

const keyringService = "cw"

// Store is one context's token storage.
type Store struct {
	edge    string
	context string
	subject string
}

// New builds a Store for an edge + context name + subject (the keychain key).
func New(edge, context, subject string) *Store {
	return &Store{edge: edge, context: context, subject: subject}
}

func (s *Store) keyringKey() string { return s.edge + "|" + s.subject }

func (s *Store) accessPath() string {
	return filepath.Join(config.Dir(), "tokens", s.context+".json")
}

// SaveRefresh stores the refresh token in the keychain.
func (s *Store) SaveRefresh(token string) error {
	if err := keyring.Set(keyringService, s.keyringKey(), token); err != nil {
		return fmt.Errorf("tokenstore: keychain set: %w", err)
	}
	return nil
}

// Refresh reads the refresh token from the keychain.
func (s *Store) Refresh() (string, error) {
	tok, err := keyring.Get(keyringService, s.keyringKey())
	if err != nil {
		return "", fmt.Errorf("tokenstore: keychain get: %w", err)
	}
	return tok, nil
}

type accessCache struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// SaveAccess caches the access token + its absolute expiry (0600).
func (s *Store) SaveAccess(token string, expiresAt time.Time) error {
	if err := os.MkdirAll(filepath.Dir(s.accessPath()), 0o700); err != nil {
		return fmt.Errorf("tokenstore: mkdir: %w", err)
	}
	b, _ := json.Marshal(accessCache{AccessToken: token, ExpiresAt: expiresAt})
	if err := os.WriteFile(s.accessPath(), b, 0o600); err != nil {
		return fmt.Errorf("tokenstore: write access: %w", err)
	}
	return nil
}

// Access returns the cached access token + expiry, or an error if absent.
func (s *Store) Access() (string, time.Time, error) {
	b, err := os.ReadFile(s.accessPath())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("tokenstore: read access: %w", err)
	}
	var c accessCache
	if err := json.Unmarshal(b, &c); err != nil {
		return "", time.Time{}, fmt.Errorf("tokenstore: parse access: %w", err)
	}
	return c.AccessToken, c.ExpiresAt, nil
}

// Clear removes both tokens. Best-effort on a missing access cache.
func (s *Store) Clear() error {
	kerr := keyring.Delete(keyringService, s.keyringKey())
	if kerr != nil && !errors.Is(kerr, keyring.ErrNotFound) {
		return fmt.Errorf("tokenstore: keychain delete: %w", kerr)
	}
	if err := os.Remove(s.accessPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tokenstore: remove access: %w", err)
	}
	return nil
}
