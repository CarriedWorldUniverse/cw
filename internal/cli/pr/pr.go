// Package pr implements `cw pr`: create, list, view, merge.
package pr

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/cairn"
	"github.com/CarriedWorldUniverse/cw/internal/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
	"github.com/spf13/cobra"
)

// NewCmd builds the `cw pr` command tree.
func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "pr", Short: "Manage cairn pull requests"}
	cmd.AddCommand(newCreateCmd(gf), newListCmd(gf), newViewCmd(gf), newMergeCmd(gf))
	return cmd
}

// resolve builds a session + the (org, slug) for a pr command: --repo wins,
// else infer from the cwd's origin remote.
func resolve(gf *cmdutil.GlobalFlags, repoFlag, orgFlag string) (*client.Client, config.Context, string, string, error) {
	c, ctx, _, err := cmdutil.Session(gf)
	if err != nil {
		return nil, config.Context{}, "", "", err
	}
	ref := repoFlag
	if ref == "" {
		o, s, ok := cmdutil.InferRepoFromCwd()
		if !ok {
			return nil, config.Context{}, "", "", fmt.Errorf("specify --repo <org>/<slug> (not inside a cairn clone)")
		}
		return c, ctx, o, s, nil
	}
	org, slug, err := cmdutil.ResolveRepo(ref, orgFlag, ctx.Identity.Org)
	return c, ctx, org, slug, err
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var repoFlag, orgFlag, head, base, title, body, dod, project string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open a pull request (creates a ledger issue)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Fail fast on missing required flags before building a session / inferring the repo.
			if head == "" || base == "" || title == "" || project == "" {
				return fmt.Errorf("--head, --base, --title, --project are required")
			}
			c, _, org, slug, err := resolve(gf, repoFlag, orgFlag)
			if err != nil {
				return err
			}
			p, err := cairn.OpenPull(cmd.Context(), c, org, slug, cairn.OpenPullInput{
				Source: head, Target: base, Title: title, Description: body, Project: project, DefinitionOfDone: dod,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "opened PR %s (%s→%s), ledger issue %s\n", p.ID, p.Source, p.Target, p.LedgerIssueKey)
			fmt.Println(p.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&repoFlag, "repo", "", "repo <org>/<slug> (default: infer from cwd)")
	f.StringVar(&orgFlag, "org", "", "org id override")
	f.StringVar(&head, "head", "", "source branch (required)")
	f.StringVar(&base, "base", "", "target branch (required)")
	f.StringVar(&title, "title", "", "PR title (required)")
	f.StringVar(&body, "body", "", "PR description")
	f.StringVar(&project, "project", "", "ledger project key (required)")
	f.StringVar(&dod, "dod", "", "definition of done")
	return cmd
}

func newListCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var repoFlag, orgFlag, state string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, org, slug, err := resolve(gf, repoFlag, orgFlag)
			if err != nil {
				return err
			}
			pulls, err := cairn.ListPulls(cmd.Context(), c, org, slug, state)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(pulls)
			}
			for _, p := range pulls {
				fmt.Printf("%-4s %-8s %s→%s  %s\n", p.ID, p.State, p.Source, p.Target, p.Title)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&repoFlag, "repo", "", "repo <org>/<slug>")
	f.StringVar(&orgFlag, "org", "", "org id override")
	f.StringVar(&state, "state", "open", "open|merged|all")
	return cmd
}

func newViewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var repoFlag, orgFlag string
	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "Show a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, org, slug, err := resolve(gf, repoFlag, orgFlag)
			if err != nil {
				return err
			}
			p, err := cairn.GetPull(cmd.Context(), c, org, slug, args[0])
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(p)
			}
			fmt.Printf("id:     %s\nstate:  %s\nbranch: %s → %s\ntitle:  %s\nissue:  %s\n", p.ID, p.State, p.Source, p.Target, p.Title, p.LedgerIssueKey)
			return nil
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "repo <org>/<slug>")
	cmd.Flags().StringVar(&orgFlag, "org", "", "org id override")
	return cmd
}

func newMergeCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var repoFlag, orgFlag string
	cmd := &cobra.Command{
		Use:   "merge <id>",
		Short: "Merge a pull request (fast-forward only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, org, slug, err := resolve(gf, repoFlag, orgFlag)
			if err != nil {
				return err
			}
			res, err := cairn.MergePull(cmd.Context(), c, org, slug, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "merged PR %s → %s @ %s\n", res.ID, res.Target, res.MergedSHA)
			if res.LedgerCommentError != "" {
				fmt.Fprintf(os.Stderr, "warning: ledger comment failed: %s\n", res.LedgerCommentError)
			}
			fmt.Println(res.MergedSHA)
			return nil
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "repo <org>/<slug>")
	cmd.Flags().StringVar(&orgFlag, "org", "", "org id override")
	return cmd
}
