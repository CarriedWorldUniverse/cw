// Package cmdutil holds CLI plumbing shared across cw command groups: the
// global flags, the authed-client session builder, and repo-ref helpers.
package cmdutil

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/CarriedWorldUniverse/cw/internal/oidc"
	"github.com/CarriedWorldUniverse/cw/internal/tokenstore"
)

// GlobalFlags carries the root persistent flags every command reads (precedence
// flag > env > current context). cobra populates the fields at Execute time.
type GlobalFlags struct {
	Context  string
	Edge     string
	Token    string
	Identity string
	JSON     bool
}

// Session resolves the effective context and builds a client for it. With a
// static --token it returns a token-only client (no store).
func Session(gf *GlobalFlags) (*client.Client, config.Context, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Context{}, "", err
	}
	ctx, name, err := cfg.Resolve(gf.Context, gf.Edge)
	if err != nil {
		return nil, config.Context{}, "", err
	}
	if gf.Token != "" {
		return client.WithStaticToken(ctx.Edge, gf.Token), ctx, name, nil
	}
	store := tokenstore.New(ctx.Edge, name, ctx.Identity.Subject)
	return client.New(ctx.Edge, store, oidc.New(ctx.Edge)), ctx, name, nil
}

// ResolveRepo resolves a repo reference to (org, slug). ref is "<slug>" or
// "<org>/<slug>"; flagOrg (--org) overrides any org; defOrg (the context's org)
// is the fallback. Errors if ref is empty or no org can be determined.
func ResolveRepo(ref, flagOrg, defOrg string) (org, slug string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("repo reference required (<slug> or <org>/<slug>)")
	}
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		org, slug = ref[:i], ref[i+1:]
	} else {
		slug = ref
	}
	if flagOrg != "" {
		org = flagOrg
	}
	if org == "" {
		org = defOrg
	}
	if org == "" {
		return "", "", fmt.Errorf("no org for repo %q: pass <org>/%s or --org, or log in so the context has an org", ref, slug)
	}
	if slug == "" {
		return "", "", fmt.Errorf("empty repo slug in %q", ref)
	}
	return org, slug, nil
}

// CairnGitURL builds the Smart-HTTP git URL for a repo through the edge.
func CairnGitURL(edge, org, slug string) string {
	return strings.TrimRight(edge, "/") + "/cairn/" + org + "/" + slug + ".git"
}

// ParseCairnGitURL extracts (org, slug) from a cairn Smart-HTTP remote URL
// (".../cairn/<org>/<slug>.git"), or ok=false.
func ParseCairnGitURL(remote string) (org, slug string, ok bool) {
	i := strings.Index(remote, "/cairn/")
	if i < 0 || !strings.HasSuffix(remote, ".git") {
		return "", "", false
	}
	rest := strings.TrimSuffix(remote[i+len("/cairn/"):], ".git")
	j := strings.IndexByte(rest, '/')
	if j <= 0 || j == len(rest)-1 {
		return "", "", false
	}
	return rest[:j], rest[j+1:], true
}

// InferRepoFromCwd reads `git remote get-url origin` in the cwd and parses a
// cairn (org, slug) from it. ok=false when not in a cairn clone.
func InferRepoFromCwd() (org, slug string, ok bool) {
	g, err := exec.LookPath("git")
	if err != nil {
		return "", "", false
	}
	out, err := exec.Command(g, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", false
	}
	return ParseCairnGitURL(strings.TrimSpace(string(out)))
}
