// Package prompt holds cw's interactive terminal prompts: it reads a human's
// email+password (no-echo) from a TTY. Interactive only — the password read
// requires a terminal; for non-interactive use present a bearer with --token or
// log in as an agent.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
