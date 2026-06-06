package setupgit

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
)

func TestResolveHostExplicit(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")

	got, err := resolveHost("github")
	if err != nil {
		t.Fatalf("resolveHost(github): %v", err)
	}
	if got.Name != "github" || got.CredentialID != "github.com" || got.Helper != githubHelper {
		t.Fatalf("github host: %+v", got)
	}

	got, err = resolveHost("cairn")
	if err != nil {
		t.Fatalf("resolveHost(cairn): %v", err)
	}
	if got.Name != "cairn" || !strings.Contains(got.Helper, "cairn agent-git auth not yet supported") {
		t.Fatalf("cairn host: %+v", got)
	}
}

func TestResolveHostPrimaryDefault(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	cfg := &config.Config{Git: config.Git{PrimaryHost: "cairn"}, Contexts: map[string]config.Context{}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := resolveHost("")
	if err != nil {
		t.Fatalf("resolveHost(default): %v", err)
	}
	if got.Name != "cairn" {
		t.Fatalf("default host = %q, want cairn", got.Name)
	}

	t.Setenv("CW_PRIMARY_GIT_HOST", "github")
	got, err = resolveHost("")
	if err != nil {
		t.Fatalf("resolveHost(env): %v", err)
	}
	if got.Name != "github" {
		t.Fatalf("env host = %q, want github", got.Name)
	}
}

func TestSetupGitGithubWritesHelperAndIdentity(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	saveContext(t, "builder")

	got, _, err := captureSetupGit(&cmdutil.GlobalFlags{}, "github")
	if err != nil {
		t.Fatalf("setup-git github: %v", err)
	}
	if got["credential.helper"] != githubHelper {
		t.Fatalf("helper = %q, want %q", got["credential.helper"], githubHelper)
	}
	if got["user.name"] != "builder" {
		t.Fatalf("user.name = %q", got["user.name"])
	}
	if got["user.email"] != "builder@agents.carriedworld.com" {
		t.Fatalf("user.email = %q", got["user.email"])
	}
}

func TestSetupGitGithubAuthenticatesGHWithFetchedToken(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	saveContext(t, "builder")

	cmd := &cobra.Command{Use: "setup-git"}
	var fetchedHost string
	var calls []ghCall
	writes := map[string]string{}
	err := runSetupGit(cmd, &cmdutil.GlobalFlags{}, "github",
		func(key, value string) error {
			writes[key] = value
			return nil
		},
		func(_ *cmdutil.GlobalFlags, host string) (string, string, error) {
			fetchedHost = host
			return "x-access-token", "gh-token", nil
		},
		func(file string) (string, error) {
			if file != "gh" {
				t.Fatalf("looked up executable = %q, want gh", file)
			}
			return "/bin/gh", nil
		},
		func(name string, args []string, stdin string) error {
			calls = append(calls, ghCall{name: name, args: append([]string(nil), args...), stdin: stdin})
			return nil
		},
	)
	if err != nil {
		t.Fatalf("setup-git github: %v", err)
	}
	if fetchedHost != "github.com" {
		t.Fatalf("fetched host = %q, want github.com", fetchedHost)
	}
	if len(calls) != 2 {
		t.Fatalf("gh calls = %#v, want 2 calls", calls)
	}
	if calls[0].name != "/bin/gh" || strings.Join(calls[0].args, " ") != "auth login --with-token" || calls[0].stdin != "gh-token" {
		t.Fatalf("gh login call = %#v", calls[0])
	}
	if calls[1].name != "/bin/gh" || strings.Join(calls[1].args, " ") != "auth setup-git" || calls[1].stdin != "" {
		t.Fatalf("gh setup-git call = %#v", calls[1])
	}
	if writes["credential.helper"] != githubHelper {
		t.Fatalf("git config not preserved: %v", writes)
	}
}

func TestSetupGitGithubSkipsGHWhenMissing(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	saveContext(t, "builder")

	cmd := &cobra.Command{Use: "setup-git"}
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)
	fetchCalled := false
	err := runSetupGit(cmd, &cmdutil.GlobalFlags{}, "github",
		func(string, string) error { return nil },
		func(_ *cmdutil.GlobalFlags, host string) (string, string, error) {
			fetchCalled = true
			if host != "github.com" {
				t.Fatalf("fetched host = %q, want github.com", host)
			}
			return "x-access-token", "gh-token", nil
		},
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(string, []string, string) error {
			t.Fatal("gh runner should not be called when gh is missing")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("setup-git github with missing gh: %v", err)
	}
	if fetchCalled {
		t.Fatal("fetched github credential when gh was missing")
	}
	if !strings.Contains(errOut.String(), "gh CLI not found") {
		t.Fatalf("missing warning: %q", errOut.String())
	}
}

func TestSetupGitCairnWritesIdentityAndStub(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	saveContext(t, "builder")

	got, ghTokens, err := captureSetupGit(&cmdutil.GlobalFlags{}, "cairn")
	if err != nil {
		t.Fatalf("setup-git cairn: %v", err)
	}
	if got["user.name"] != "builder" || got["user.email"] != "builder@agents.carriedworld.com" {
		t.Fatalf("identity not written: %v", got)
	}
	if !strings.Contains(got["credential.helper"], "cairn agent-git auth not yet supported") {
		t.Fatalf("cairn helper missing stub note: %q", got["credential.helper"])
	}
	if len(ghTokens) != 0 {
		t.Fatalf("cairn touched gh: %v", ghTokens)
	}
}

func TestSetupGitBareUsesPrimary(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	cfg := &config.Config{
		CurrentContext: "agent",
		Git:            config.Git{PrimaryHost: "cairn"},
		Contexts: map[string]config.Context{
			"agent": {Edge: "http://edge", Identity: config.Identity{Kind: "agent", Subject: "a1", Display: "builder", Slug: "builder"}},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, _, err := captureSetupGit(&cmdutil.GlobalFlags{}, "")
	if err != nil {
		t.Fatalf("setup-git default: %v", err)
	}
	if !strings.Contains(got["credential.helper"], "cairn agent-git auth not yet supported") {
		t.Fatalf("bare setup-git did not use primary cairn: %v", got)
	}
}

func TestSetupGitIdentityFromToken(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	token := "x." + b64(`{"sub":"a1","kind":"agent","slug":"token-builder"}`) + ".y"

	got, _, err := captureSetupGit(&cmdutil.GlobalFlags{Token: token}, "github")
	if err != nil {
		t.Fatalf("setup-git token identity: %v", err)
	}
	if got["user.name"] != "token-builder" {
		t.Fatalf("user.name = %q, want token-builder", got["user.name"])
	}
}

func TestSetupGitIdentityFromFile(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	path := t.TempDir() + "/identity.yaml"
	if err := os.WriteFile(path, []byte("slug: file-builder\n"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	got, _, err := captureSetupGit(&cmdutil.GlobalFlags{Identity: path}, "github")
	if err != nil {
		t.Fatalf("setup-git file identity: %v", err)
	}
	if got["user.name"] != "file-builder" {
		t.Fatalf("user.name = %q, want file-builder", got["user.name"])
	}
}

func captureSetupGit(gf *cmdutil.GlobalFlags, host string) (map[string]string, []string, error) {
	cmd := &cobra.Command{Use: "setup-git"}
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)
	writes := map[string]string{}
	var ghTokens []string
	err := runSetupGit(cmd, gf, host, func(key, value string) error {
		writes[key] = value
		return nil
	}, func(_ *cmdutil.GlobalFlags, host string) (string, string, error) {
		return "x-access-token", "token-for-" + host, nil
	}, func(string) (string, error) {
		return "/bin/gh", nil
	}, func(_ string, args []string, stdin string) error {
		if strings.Join(args, " ") == "auth login --with-token" {
			ghTokens = append(ghTokens, stdin)
		}
		return nil
	})
	return writes, ghTokens, err
}

type ghCall struct {
	name  string
	args  []string
	stdin string
}

func saveContext(t *testing.T, slug string) {
	t.Helper()
	cfg := &config.Config{
		CurrentContext: "agent",
		Contexts: map[string]config.Context{
			"agent": {Edge: "http://edge", Identity: config.Identity{Kind: "agent", Subject: "a1", Display: slug, Slug: slug}},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func b64(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
