// Package human implements `cw human`: create and password management (herald admin).
package human

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cwb-client/herald"
	"github.com/CarriedWorldUniverse/cwb-client/identity"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/prompt"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "human", Short: "Provision human identities (herald admin)"}
	cmd.AddCommand(newCreateCmd(gf), newPasswordCmd(gf), newSetPasswordCmd(gf))
	return cmd
}

// readSecret sources a password: if passwordStdin, read one trimmed line from r;
// else if r is an interactive TTY, prompt no-echo; else (required) error, or
// (optional) return "". required distinguishes set-password (must) from a
// create without --password-stdin (may skip).
func readSecret(r io.Reader, passwordStdin, required bool) (string, error) {
	if passwordStdin {
		line, err := bufio.NewReader(r).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		pw := strings.TrimRight(line, "\r\n")
		if pw == "" && required {
			return "", fmt.Errorf("empty password on stdin")
		}
		return pw, nil
	}
	if f, ok := r.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return prompt.PromptPassword(f, "Password")
	}
	if required {
		return "", fmt.Errorf("provide the password via --password-stdin or an interactive terminal")
	}
	return "", nil
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var org, name string
	var scopes []string
	var passwordStdin bool
	cmd := &cobra.Command{
		Use:   "create --org <org> --name <display-name>",
		Short: "Create a human (optionally setting a password from stdin)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if org == "" || name == "" {
				return fmt.Errorf("--org and --name are required")
			}
			// A password on create is opt-in via --password-stdin only; without it
			// the human is created password-less (set one later with set-password).
			// There is deliberately no interactive prompt here — create is often
			// scripted. (readSecret's TTY-prompt branch is used by set-password.)
			var pw string
			if passwordStdin {
				var err error
				if pw, err = readSecret(cmd.InOrStdin(), true, true); err != nil {
					return err
				}
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			h, err := herald.CreateHuman(cmd.Context(), c, org, herald.CreateHumanInput{DisplayName: name, Scopes: scopes})
			if err != nil {
				return err
			}
			msg := fmt.Sprintf("created human %s (%s) in org %s", h.ID, h.DisplayName, h.Org)
			if passwordStdin {
				if err := herald.SetHumanPassword(cmd.Context(), c, h.ID, pw); err != nil {
					return fmt.Errorf("human created (%s) but set-password failed: %w", h.ID, err)
				}
				msg += "; password set"
			}
			fmt.Fprintln(os.Stderr, msg)
			fmt.Println(h.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&org, "org", "", "org id (required)")
	f.StringVar(&name, "name", "", "display name (required)")
	f.StringArrayVar(&scopes, "scope", nil, "scope to grant, e.g. knowledge:read (repeatable)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the human's password from stdin")
	return cmd
}

func newSetPasswordCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var passwordStdin bool
	cmd := &cobra.Command{
		Use:   "set-password <human-id>",
		Short: "Set (or reset) a human's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := readSecret(cmd.InOrStdin(), passwordStdin, true)
			if err != nil {
				return err
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			if err := herald.SetHumanPassword(cmd.Context(), c, args[0], pw); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "password set for %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin (else prompt)")
	return cmd
}

func newPasswordCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "password",
		Short: "Manage a human's login password",
	}
	cmd.AddCommand(newPasswordSetCmd(gf))
	return cmd
}

func newPasswordSetCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set (or reset) a human's herald login password",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, sess, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			target := strings.TrimSpace(id)
			if target == "" {
				target, err = callerSubject(cmd.Context(), c, sess.Identity.Subject)
				if err != nil {
					return err
				}
			}
			pw, err := promptConfirmedPassword()
			if err != nil {
				return err
			}
			if err := setHumanPassword(cmd.Context(), c, target, pw); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "password set for %s\n", target)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "human id or display name (defaults to the caller's subject)")
	return cmd
}

type passwordPrompt func(label string) (string, error)

var promptConfirmedPassword = promptConfirmedPasswordTTY

func promptConfirmedPasswordTTY() (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("password prompt needs /dev/tty: %w", err)
	}
	defer tty.Close()
	return readConfirmedPassword(func(label string) (string, error) {
		return prompt.PromptPassword(tty, label)
	})
}

func readConfirmedPassword(read passwordPrompt) (string, error) {
	pw, err := read("Password")
	if err != nil {
		return "", err
	}
	if len(pw) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	confirm, err := read("Confirm password")
	if err != nil {
		return "", err
	}
	if pw != confirm {
		return "", fmt.Errorf("passwords do not match")
	}
	return pw, nil
}

func callerSubject(ctx context.Context, c *client.Client, configuredSubject string) (string, error) {
	tok, err := c.AccessToken(ctx)
	if err == nil {
		claims, err := identity.DecodeAccessClaims(tok)
		if err == nil {
			if sub, _ := claims["sub"].(string); sub != "" {
				return sub, nil
			}
		}
	}
	if configuredSubject != "" {
		return configuredSubject, nil
	}
	if err != nil {
		return "", fmt.Errorf("--id is required when caller subject cannot be resolved: %w", err)
	}
	return "", fmt.Errorf("--id is required when caller subject cannot be resolved")
}

func setHumanPassword(ctx context.Context, c client.Doer, id, password string) error {
	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		return fmt.Errorf("human password set: marshal: %w", err)
	}
	path := "/api/humans/" + url.PathEscape(id) + "/password"
	resp, respBody, err := c.Do(ctx, http.MethodPost, "herald", path, body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 == 2 {
		return nil
	}
	msg := responseError(respBody)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("human password set: unauthorized (401): %s", msg)
	case http.StatusForbidden:
		return fmt.Errorf("human password set: forbidden (403): %s", msg)
	default:
		return fmt.Errorf("human password set: status %d: %s", resp.StatusCode, msg)
	}
}

func responseError(body []byte) string {
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
	case strings.TrimSpace(string(body)) != "":
		return strings.TrimSpace(string(body))
	default:
		return "no response body"
	}
}
