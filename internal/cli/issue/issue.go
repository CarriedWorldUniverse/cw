// Package issue implements `cw issue`: create, list, view, claim, transition, comment.
package issue

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/ledger"
	"github.com/spf13/cobra"
)

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "issue", Short: "Manage ledger issues"}
	cmd.AddCommand(newCreateCmd(gf), newListCmd(gf), newViewCmd(gf), newClaimCmd(gf), newTransitionCmd(gf), newCommentCmd(gf))
	return cmd
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var project, typ, title, body, dod, priority string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an issue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" || typ == "" || title == "" {
				return fmt.Errorf("--project, --type, --title are required")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			iss, err := ledger.CreateIssue(cmd.Context(), c, ledger.CreateInput{
				Project: project, Type: typ, Summary: title, Description: body, DefinitionOfDone: dod, Priority: priority,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "created %s (%s) in %s\n", iss.Key, iss.Status, iss.Project)
			fmt.Println(iss.Key)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&project, "project", "", "project key, e.g. NEX (required)")
	f.StringVar(&typ, "type", "", "issue type, e.g. Story (required)")
	f.StringVar(&title, "title", "", "summary (required)")
	f.StringVar(&body, "body", "", "description")
	f.StringVar(&dod, "dod", "", "definition of done")
	f.StringVar(&priority, "priority", "", "priority")
	return cmd
}

func newListCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var mine, ready bool
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues (--mine [default] / --ready / --project <KEY>)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			n := 0
			if mine {
				n++
			}
			if ready {
				n++
			}
			if project != "" {
				n++
			}
			if n > 1 {
				return fmt.Errorf("specify one of --mine / --ready / --project")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			var refs []ledger.IssueRef
			switch {
			case ready:
				refs, err = ledger.ListReady(cmd.Context(), c)
			case project != "":
				refs, err = ledger.SearchByProject(cmd.Context(), c, project)
			default: // --mine (default)
				refs, err = ledger.ListMine(cmd.Context(), c)
			}
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(refs)
			}
			for _, r := range refs {
				fmt.Printf("%-12s %-12s %s\n", r.Key, r.Status, r.Summary)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&mine, "mine", false, "issues assigned to you (default)")
	f.BoolVar(&ready, "ready", false, "issues ready to work")
	f.StringVar(&project, "project", "", "all issues in a project")
	return cmd
}

func newViewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <key>",
		Short: "Show an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			iss, err := ledger.GetIssue(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(iss)
			}
			fmt.Printf("key:      %s\ntype:     %s\nstatus:   %s\nsummary:  %s\nassignee: %s\nproject:  %s\n\n%s\n\ndod:      %s\n",
				iss.Key, iss.Type, iss.Status, iss.Summary, iss.AssigneeAspect, iss.Project, iss.Description, iss.DefinitionOfDone)
			return nil
		},
	}
	return cmd
}

func newClaimCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "claim <key>",
		Short: "Claim an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			iss, err := ledger.Claim(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "claimed %s (now %s)\n", iss.Key, iss.Status)
			return nil
		},
	}
}

func newTransitionCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "transition <key> <status>",
		Short: "Move an issue to a status (e.g. \"In Review\", \"Done\")",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			if err := ledger.Transition(cmd.Context(), c, args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "%s → %s\n", args[0], args[1])
			return nil
		},
	}
}

func newCommentCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "comment <key> <body>",
		Short: "Comment on an issue",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			if err := ledger.Comment(cmd.Context(), c, args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "commented on %s\n", args[0])
			return nil
		},
	}
}
