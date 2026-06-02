// Package human implements `cw human`: create and set-password (herald admin).
package human

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "human", Short: "Provision human identities (herald admin)"}
	cmd.AddCommand(newCreateCmd(gf), newSetPasswordCmd(gf))
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
		return identity.PromptPassword(f, "Password")
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
