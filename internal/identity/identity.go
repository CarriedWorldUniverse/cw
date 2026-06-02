// Package identity produces the credentials cw presents to herald: it prompts a
// human for email+password (no-echo), signs an agent's casket jwt-bearer
// assertion, and decodes (without verifying) access-token claims for display.
package identity

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	casket "github.com/CarriedWorldUniverse/casket-go"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/term"
)

// PromptHuman reads an email/username + password from the terminal (password
// not echoed). Interactive only — the password read requires a TTY; for
// non-interactive use present a bearer with --token or log in as an agent.
func PromptHuman(in *os.File) (username, password string, err error) {
	fmt.Fprint(os.Stderr, "Email: ")
	// ReadString handles an EOF-terminated line (Fscanln spuriously errors on it).
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", "", fmt.Errorf("identity: read username: %w", err)
	}
	username = strings.TrimSpace(line)
	if username == "" {
		return "", "", errors.New("identity: empty email")
	}
	fmt.Fprint(os.Stderr, "Password: ")
	pw, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		if !term.IsTerminal(int(in.Fd())) {
			return "", "", fmt.Errorf("identity: password prompt needs a terminal (piped input not supported; use --token or --agent): %w", err)
		}
		return "", "", fmt.Errorf("identity: read password: %w", err)
	}
	return username, string(pw), nil
}

// PromptPassword reads a single password from the terminal without echoing it.
// Interactive only — the read requires a TTY.
func PromptPassword(in *os.File, label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	pw, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		if !term.IsTerminal(int(in.Fd())) {
			return "", fmt.Errorf("identity: password prompt needs a terminal: %w", err)
		}
		return "", fmt.Errorf("identity: read password: %w", err)
	}
	return string(pw), nil
}

// AgentAssertion derives the agent's casket key from (seed, slug) and signs an
// RFC 7523 jwt-bearer assertion (iss=sub=agentID, aud=tokenURL, 2-minute exp).
// now defaults to time.Now; injectable for tests via AgentAssertionAt.
func AgentAssertion(seed []byte, slug, agentID, tokenURL string) (string, error) {
	return AgentAssertionAt(seed, slug, agentID, tokenURL, time.Now())
}

// AgentAssertionAt is AgentAssertion with an explicit clock.
func AgentAssertionAt(seed []byte, slug, agentID, tokenURL string, now time.Time) (string, error) {
	if len(seed) == 0 || slug == "" || agentID == "" || tokenURL == "" {
		return "", errors.New("identity: seed, slug, agentID, tokenURL all required")
	}
	priv, _, err := casket.DeriveAgentKey(seed, slug)
	if err != nil {
		return "", fmt.Errorf("identity: derive key: %w", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.EdDSA, Key: priv},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", fmt.Errorf("identity: signer: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"iss": agentID, "sub": agentID, "aud": tokenURL,
		"iat": now.Unix(), "exp": now.Add(2 * time.Minute).Unix(),
	})
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("identity: sign: %w", err)
	}
	return obj.CompactSerialize()
}

// DecodeAccessClaims decodes a JWT's claim set WITHOUT verifying the signature
// (the token came from herald; cw only reads it for display + expiry). Returns
// the claims map.
func DecodeAccessClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("identity: not a JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("identity: decode claims: %w", err)
	}
	return DecodeClaimsBytes(raw), nil
}

// DecodeClaimsBytes unmarshals a JSON claim set, returning a non-nil (possibly
// empty) map even on invalid JSON — callers read claims directly.
func DecodeClaimsBytes(raw []byte) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}
