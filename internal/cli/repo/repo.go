// Package repo implements `cw repo`: create, list, clone.
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/CarriedWorldUniverse/cw/internal/cairn"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmd builds the `cw repo` command tree.
func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "repo", Short: "Manage cairn repositories"}
	cmd.AddCommand(newCreateCmd(gf), newListCmd(gf), newCloneCmd(gf))
	return cmd
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var orgFlag string
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a repository in your org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, ctx, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			org, slug, err := cmdutil.ResolveRepo(args[0], orgFlag, ctx.Identity.Org)
			if err != nil {
				return err
			}
			rp, err := cairn.CreateRepo(cmd.Context(), c, org, slug)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "created %s/%s\n", rp.Org, rp.Slug)
			fmt.Println(cmdutil.CairnGitURL(ctx.Edge, rp.Org, rp.Slug))
			return nil
		},
	}
	cmd.Flags().StringVar(&orgFlag, "org", "", "org id (default: your context's org)")
	return cmd
}

func newListCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var orgFlag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List repositories in your org",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, ctx, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			org := orgFlag
			if org == "" {
				org = ctx.Identity.Org
			}
			if org == "" {
				return fmt.Errorf("no org: pass --org or log in so the context has an org")
			}
			repos, err := cairn.ListRepos(cmd.Context(), c, org)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(repos)
			}
			for _, r := range repos {
				fmt.Printf("%-30s %s\n", r.Slug, r.DefaultBranch)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&orgFlag, "org", "", "org id (default: your context's org)")
	return cmd
}

func newCloneCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var orgFlag string
	cmd := &cobra.Command{
		Use:   "clone <ref> [dir]",
		Short: "Clone a repository (shells git with a fresh bearer)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, ctx, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			org, slug, err := cmdutil.ResolveRepo(args[0], orgFlag, ctx.Identity.Org)
			if err != nil {
				return err
			}
			tok, err := c.AccessToken(cmd.Context())
			if err != nil {
				return err
			}
			url := cmdutil.CairnGitURL(ctx.Edge, org, slug)
			gitArgs := []string{"-c", "http.extraHeader=Authorization: Bearer " + tok, "clone", url}
			if len(args) == 2 {
				gitArgs = append(gitArgs, args[1])
			}
			return runGit(cmd.Context(), gitArgs...)
		},
	}
	cmd.Flags().StringVar(&orgFlag, "org", "", "org id (default: your context's org)")
	return cmd
}

// runGit shells out to the system git, streaming its stdio.
func runGit(ctx context.Context, args ...string) error {
	g, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	c := exec.CommandContext(ctx, g, args...)
	c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := c.Run(); err != nil {
		return fmt.Errorf("git %v: %w", args[len(args)-2:], err)
	}
	return nil
}
