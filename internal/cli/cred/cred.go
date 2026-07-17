// Package cred implements `cw cred`: explicit namespace credential lookup.
//
// Local tier (personal/<name>) is a passphrase-encrypted satchel on disk.
// Remote tier (<org>/<name>) talks to custodian's CredentialService over the
// in-mesh mTLS transport (see remote.go) — the same sovereign transport
// `cw kb`/`cw config` use.
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

var validKinds = map[string]bool{"git": true, "oauth": true, "secret": true}

var promptPassphrase = promptPassphraseTTY

// Store is the local-tier seam.
type Store interface {
	Put(name string, plaintext []byte, passphrase string) error
	Get(name string, passphrase string) ([]byte, error)
	List() ([]string, error)
	Delete(name string) error
}

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cred",
		Short: "Get, put, list, and remove explicitly namespaced credentials",
	}
	cmd.AddCommand(newGetCmd(), newPutCmd(), newListCmd(), newRmCmd())
	return cmd
}

func validateKind(kind string) error {
	if !validKinds[kind] {
		return fmt.Errorf("cred: --kind must be one of git, oauth, secret (got %q)", kind)
	}
	return nil
}

func newGetCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "get <namespace>/<name>",
		Short: "Print a credential secret to stdout",
		Long: "Print a credential to stdout.\n" +
			"Local (personal/<name>): always the decrypted secret bytes, no decoration.\n" +
			"Remote (<org>/<name>): --kind secret (default) prints the raw secret value;\n" +
			"--kind git|oauth prints the (non-secret) bundle fields as JSON, since those\n" +
			"kinds are structured rather than a single opaque value.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			route, err := routeName(args[0])
			if err != nil {
				return err
			}
			if route.remote {
				if err := validateKind(kind); err != nil {
					return err
				}
				return remoteGet(cmd.Context(), cmd.OutOrStdout(), route.namespace, route.name, kind)
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
	cmd.Flags().StringVar(&kind, "kind", "secret", "remote tier only: git | oauth | secret")
	return cmd
}

func newPutCmd() *cobra.Command {
	var kind, host, username string
	cmd := &cobra.Command{
		Use:   "put <namespace>/<name>",
		Short: "Store a credential secret from stdin",
		Long: "Store a credential secret read from stdin.\n" +
			"Remote (<org>/<name>) only supports --kind secret in this release — --host\n" +
			"and --username are stored as optional (non-secret) hints alongside the value.\n" +
			"git/oauth remote writes are out of scope here.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			route, err := routeName(args[0])
			if err != nil {
				return err
			}
			if route.remote {
				if err := validateKind(kind); err != nil {
					return err
				}
			}
			secret, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("cred: read secret from stdin: %w", err)
			}
			if route.remote {
				return remotePut(cmd.Context(), route.namespace, route.name, kind, secret, host, username)
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
	cmd.Flags().StringVar(&kind, "kind", "secret", "remote tier only: secret (git/oauth writes are out of scope)")
	cmd.Flags().StringVar(&host, "host", "", "remote tier only: optional hint — the service/API host this secret belongs to")
	cmd.Flags().StringVar(&username, "username", "", "remote tier only: optional hint — header/account/usage (e.g. x-api-key, bearer)")
	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls <namespace>",
		Short: "List credential names in a namespace",
		Long: "List credentials in a namespace.\n" +
			"Local (personal): bare names, one per line.\n" +
			"Remote (<org>): kind/name, one per line — kinds share the namespace remotely.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			route, err := routeList(args[0])
			if err != nil {
				return err
			}
			if route.remote {
				return remoteList(cmd.Context(), cmd.OutOrStdout(), route.namespace)
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

func newRmCmd() *cobra.Command {
	var kind string
	var yes bool
	cmd := &cobra.Command{
		Use:   "rm <namespace>/<name> --yes",
		Short: "Remove a credential (irreversible; requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("pass --yes to confirm deletion (irreversible)")
			}
			route, err := routeName(args[0])
			if err != nil {
				return err
			}
			if route.remote {
				if err := validateKind(kind); err != nil {
					return err
				}
				return remoteDelete(cmd.Context(), route.namespace, route.name, kind)
			}
			if err := defaultStore().Delete(route.name); err != nil {
				return formatStoreError("rm", route.ref(), err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "secret", "remote tier only: git | oauth | secret")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the irreversible delete")
	return cmd
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
