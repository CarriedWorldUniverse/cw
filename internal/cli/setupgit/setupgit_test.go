package setupgit

import (
	"bytes"
	"encoding/base64"
	"os"
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

	got, err := captureSetupGit(&cmdutil.GlobalFlags{}, "github")
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

func TestSetupGitCairnWritesIdentityAndStub(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_PRIMARY_GIT_HOST", "")
	saveContext(t, "builder")

	got, err := captureSetupGit(&cmdutil.GlobalFlags{}, "cairn")
	if err != nil {
		t.Fatalf("setup-git cairn: %v", err)
	}
	if got["user.name"] != "builder" || got["user.email"] != "builder@agents.carriedworld.com" {
		t.Fatalf("identity not written: %v", got)
	}
	if !strings.Contains(got["credential.helper"], "cairn agent-git auth not yet supported") {
		t.Fatalf("cairn helper missing stub note: %q", got["credential.helper"])
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

	got, err := captureSetupGit(&cmdutil.GlobalFlags{}, "")
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

	got, err := captureSetupGit(&cmdutil.GlobalFlags{Token: token}, "github")
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

	got, err := captureSetupGit(&cmdutil.GlobalFlags{Identity: path}, "github")
	if err != nil {
		t.Fatalf("setup-git file identity: %v", err)
	}
	if got["user.name"] != "file-builder" {
		t.Fatalf("user.name = %q, want file-builder", got["user.name"])
	}
}

func captureSetupGit(gf *cmdutil.GlobalFlags, host string) (map[string]string, error) {
	cmd := &cobra.Command{Use: "setup-git"}
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)
	writes := map[string]string{}
	err := runSetupGit(cmd, gf, host, func(key, value string) error {
		writes[key] = value
		return nil
	})
	return writes, err
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
