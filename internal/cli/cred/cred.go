// Package cred implements `cw cred`: explicit namespace credential lookup.
package cred

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/prompt"
	"github.com/spf13/cobra"
)

const personalNamespace = "personal"

var promptPassphrase = promptPassphraseTTY

// Store is the local-tier seam. A remote custodian implementation can satisfy
// the same command-level operations once NEX-650 lands.
type Store interface {
	Put(name string, plaintext []byte, passphrase string) error
	Get(name string, passphrase string) ([]byte, error)
	List() ([]string, error)
}

func NewCmd(_ *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cred",
		Short: "Get, put, and list explicitly namespaced credentials",
	}
	cmd.AddCommand(newGetCmd(), newPutCmd(), newListCmd())
	return cmd
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <namespace>/<name>",
		Short: "Print a credential secret to stdout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			route, err := routeName(args[0])
			if err != nil {
				return err
			}
			if route.remote {
				return remoteNotImplemented(route.namespace)
			}
			passphrase, err := promptPassphrase("Satchel passphrase")
			if err != nil {
				return err
			}
			secret, err := defaultStore().Get(route.name, passphrase)
			if err != nil {
				return formatStoreError("get", route.ref(), err)
			}
			_, err = cmd.OutOrStdout().Write(secret)
			return err
		},
	}
}

func newPutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "put <namespace>/<name>",
		Short: "Store a credential secret from stdin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			route, err := routeName(args[0])
			if err != nil {
				return err
			}
			if route.remote {
				return remoteNotImplemented(route.namespace)
			}
			secret, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("cred: read secret from stdin: %w", err)
			}
			passphrase, err := promptPassphrase("Satchel passphrase")
			if err != nil {
				return err
			}
			if err := defaultStore().Put(route.name, secret, passphrase); err != nil {
				return formatStoreError("put", route.ref(), err)
			}
			return nil
		},
	}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls <namespace>",
		Short: "List credential names in a namespace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			route, err := routeList(args[0])
			if err != nil {
				return err
			}
			if route.remote {
				return remoteNotImplemented(route.namespace)
			}
			names, err := defaultStore().List()
			if err != nil {
				return formatStoreError("ls", route.ref(), err)
			}
			for _, name := range names {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), name); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

type routedName struct {
	namespace string
	name      string
	remote    bool
}

func (r routedName) ref() string {
	if r.name == "" {
		return r.namespace
	}
	return r.namespace + "/" + r.name
}

func routeName(ref string) (routedName, error) {
	ref = strings.TrimSpace(ref)
	ns, name, ok := strings.Cut(ref, "/")
	if !ok {
		return routedName{}, fmt.Errorf("cred: credential name must be explicit: use personal/%s for the local satchel or <org>/%s for a custodian secret", ref, ref)
	}
	if ns == "" || name == "" || strings.Contains(name, "/") {
		return routedName{}, fmt.Errorf("cred: credential name must be <namespace>/<name>")
	}
	if err := validateSecretName(name); err != nil {
		return routedName{}, err
	}
	return routedName{namespace: ns, name: name, remote: ns != personalNamespace}, nil
}

func routeList(ref string) (routedName, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return routedName{}, fmt.Errorf("cred: namespace required: use personal or <org>")
	}
	if strings.Contains(ref, "/") {
		return routedName{}, fmt.Errorf("cred: ls expects a namespace: use personal or <org>")
	}
	return routedName{namespace: ref, remote: ref != personalNamespace}, nil
}

func validateSecretName(name string) error {
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("cred: secret name %q may contain only letters, digits, dot, underscore, and dash", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("cred: invalid secret name %q", name)
	}
	return nil
}

func remoteNotImplemented(namespace string) error {
	return fmt.Errorf("cred: namespace %q is the remote custodian tier; remote tier not implemented yet (NEX-650)", namespace)
}

func formatStoreError(op, ref string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return fmt.Errorf("cred: no such secret %s", ref)
	case errors.Is(err, ErrDecrypt):
		return fmt.Errorf("cred: decrypt failed for %s: wrong passphrase or corrupt secret", ref)
	default:
		return fmt.Errorf("cred: %s %s: %w", op, ref, err)
	}
}

func defaultStore() Store {
	return NewFileStore(satchelDir())
}

func satchelDir() string {
	if d := os.Getenv("CW_SATCHEL_DIR"); d != "" {
		return d
	}
	return filepath.Join(config.Dir(), "satchel")
}

func promptPassphraseTTY(label string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("cred: open /dev/tty for passphrase prompt: %w", err)
	}
	defer tty.Close()
	pw, err := prompt.PromptPassword(tty, label)
	if err != nil {
		return "", err
	}
	if pw == "" {
		return "", fmt.Errorf("cred: empty passphrase")
	}
	return pw, nil
}

func sortedNames(names []string) []string {
	sort.Strings(names)
	return names
}
