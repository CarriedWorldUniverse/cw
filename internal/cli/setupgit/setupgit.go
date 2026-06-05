// Package setupgit implements `cw setup-git`, the dispatched-agent equivalent
// of `gh auth setup-git`.
package setupgit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cwbidentity "github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
)

const (
	defaultPrimaryHost = "github"
	githubHelper       = "!cw credential git-helper"
	cairnHelperStub    = "!f() { echo \"TODO: cairn agent-git auth not yet supported\" >&2; exit 1; }; f"
)

type HostSpec struct {
	Name          string
	CredentialID  string
	Helper        string
	HelperMessage string
}

type gitConfigWriter func(key, value string) error

var defaultGitConfigWriter gitConfigWriter = func(key, value string) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	cmd := exec.Command(git, "config", "--global", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config --global %s: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// NewCmd builds the `setup-git` command.
func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup-git [github|cairn]",
		Short: "Configure git credentials and identity for this dispatched agent",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var host string
			if len(args) == 1 {
				host = args[0]
			}
			return runSetupGit(cmd, gf, host, defaultGitConfigWriter)
		},
	}
	return cmd
}

func runSetupGit(cmd *cobra.Command, gf *cmdutil.GlobalFlags, hostArg string, write gitConfigWriter) error {
	spec, err := ResolveHost(hostArg)
	if err != nil {
		return err
	}
	agent, err := resolveAgentName(gf)
	if err != nil {
		return err
	}
	entries := [][2]string{
		{"credential.helper", spec.Helper},
		{"user.name", agent},
		{"user.email", agent + "@agents.carriedworld.com"},
	}
	for _, entry := range entries {
		if err := write(entry[0], entry[1]); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Configured git for %s as %s\n", spec.Name, agent)
	if spec.HelperMessage != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), spec.HelperMessage)
	}
	return nil
}

func ResolveHost(arg string) (HostSpec, error) {
	host := strings.TrimSpace(strings.ToLower(arg))
	if host == "" {
		host = strings.TrimSpace(strings.ToLower(os.Getenv("CW_PRIMARY_GIT_HOST")))
	}
	if host == "" {
		cfg, err := config.Load()
		if err != nil {
			return HostSpec{}, err
		}
		host = strings.TrimSpace(strings.ToLower(cfg.Git.PrimaryHost))
	}
	if host == "" {
		host = defaultPrimaryHost
	}
	switch host {
	case "github":
		return HostSpec{Name: "github", CredentialID: "github.com", Helper: githubHelper}, nil
	case "cairn":
		return HostSpec{
			Name:          "cairn",
			CredentialID:  "cairn",
			Helper:        cairnHelperStub,
			HelperMessage: "TODO: cairn agent-git auth not yet supported",
		}, nil
	default:
		return HostSpec{}, fmt.Errorf("setup-git: host must be one of github, cairn")
	}
}

func resolveHost(arg string) (HostSpec, error) { return ResolveHost(arg) }

func resolveAgentName(gf *cmdutil.GlobalFlags) (string, error) {
	if gf.Identity != "" {
		name, err := agentNameFromIdentityFile(gf.Identity)
		if err != nil {
			return "", err
		}
		if name != "" {
			return name, nil
		}
	}
	if gf.Token != "" {
		name, err := agentNameFromToken(gf.Token)
		if err != nil {
			return "", err
		}
		if name != "" {
			return name, nil
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	ctx, _, err := cfg.Resolve(gf.Context, gf.Edge)
	if err != nil {
		return "", fmt.Errorf("setup-git: resolve identity: %w", err)
	}
	name := firstNonEmpty(ctx.Identity.Slug, ctx.Identity.Display, ctx.Identity.Subject)
	if name == "" {
		return "", fmt.Errorf("setup-git: no agent identity found (pass --identity or set CW_TOKEN)")
	}
	return name, nil
}

func agentNameFromToken(tok string) (string, error) {
	claims, err := cwbidentity.DecodeAccessClaims(tok)
	if err != nil {
		return "", fmt.Errorf("setup-git: decode CW_TOKEN: %w", err)
	}
	return firstClaimString(claims, "slug", "agent_slug", "agent", "display", "name", "sub"), nil
}

func agentNameFromIdentityFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("setup-git: read identity file: %w", err)
	}
	var raw map[string]any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(b, &raw); err != nil {
			return "", fmt.Errorf("setup-git: parse identity file: %w", err)
		}
	default:
		if err := yaml.Unmarshal(b, &raw); err != nil {
			return "", fmt.Errorf("setup-git: parse identity file: %w", err)
		}
	}
	name := firstMapString(raw, "slug", "agent_slug", "agent", "name", "display", "subject")
	for _, nested := range []string{"identity", "agent", "key"} {
		if name != "" {
			break
		}
		if m, ok := raw[nested].(map[string]any); ok {
			name = firstMapString(m, "slug", "agent_slug", "name", "display", "subject", "id")
		}
	}
	if name == "" {
		return "", fmt.Errorf("setup-git: identity file has no agent name")
	}
	return name, nil
}

func firstClaimString(claims map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, _ := claims[key].(string); s != "" {
			return s
		}
	}
	return ""
}

func firstMapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, _ := m[key].(string); s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, val := range vals {
		if val != "" {
			return val
		}
	}
	return ""
}
