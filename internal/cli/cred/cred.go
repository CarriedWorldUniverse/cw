// Package cred implements `cw cred`: explicit namespace credential lookup.
package cred

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CarriedWorldUniverse/cwb-client/client"

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

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cred",
		Short: "Get, put, and list explicitly namespaced credentials",
	}
	cmd.AddCommand(newGetCmd(gf), newPutCmd(), newListCmd())
	return cmd
}

func newGetCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
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
				c, _, _, err := cmdutil.Session(gf)
				if err != nil {
					return err
				}
				secret, err := getRemoteSecret(cmd.Context(), c, route.namespace, route.name)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write([]byte(secret))
				return err
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

func getRemoteSecret(ctx context.Context, c client.Doer, org, name string) (string, error) {
	ref := org + "/" + name
	path := "/api/secret/" + url.PathEscape(org) + "/" + url.PathEscape(name)
	resp, body, err := c.Do(ctx, http.MethodGet, "custodian", path, nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		msg := responseError(body)
		switch resp.StatusCode {
		case http.StatusForbidden:
			return "", fmt.Errorf("cred: forbidden %s: caller lacks custodian:read for that org's secret: %s", ref, msg)
		case http.StatusNotFound:
			return "", fmt.Errorf("cred: no such secret %s", ref)
		default:
			return "", fmt.Errorf("cred: get %s: status %d: %s", ref, resp.StatusCode, msg)
		}
	}
	var item struct {
		Value string `json:"value"`
		Item  struct {
			Value string `json:"value"`
		} `json:"item"`
	}
	if err := json.Unmarshal(body, &item); err != nil {
		return "", fmt.Errorf("cred: get %s: decode response: %w", ref, err)
	}
	if item.Value != "" {
		return item.Value, nil
	}
	return item.Item.Value, nil
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
