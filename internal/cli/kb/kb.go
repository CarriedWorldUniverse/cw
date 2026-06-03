// Package kb implements `cw kb`: store, search, list (commonplace knowledge).
package kb

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cwb-client/commonplace"
	"github.com/spf13/cobra"
)

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "kb", Short: "Manage commonplace knowledge"}
	cmd.AddCommand(newStoreCmd(gf), newSearchCmd(gf), newListCmd(gf), newUpdateCmd(gf), newDeleteCmd(gf))
	return cmd
}

// readContent sources the store content: --content if non-empty, else all of r
// (stdin). Errors if both are empty. If r is an interactive terminal (no piped
// stdin), it returns immediately rather than blocking on a read for EOF.
func readContent(flagContent string, r io.Reader) (string, error) {
	if flagContent != "" {
		return flagContent, nil
	}
	if f, ok := r.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return "", fmt.Errorf("provide content via --content or pipe it on stdin")
		}
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read content: %w", err)
	}
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return "", fmt.Errorf("provide content via --content or stdin")
	}
	return s, nil
}

func newStoreCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var topic, content, visibility string
	var tags []string
	cmd := &cobra.Command{
		Use:   "store --topic <t>",
		Short: "Store a knowledge entry (content from --content or stdin)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if topic == "" {
				return fmt.Errorf("--topic is required")
			}
			body, err := readContent(content, cmd.InOrStdin())
			if err != nil {
				return err
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			e, err := commonplace.Store(cmd.Context(), c, commonplace.StoreInput{
				Topic: topic, Content: body, Visibility: visibility, Tags: tags,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "stored %s (topic %q, %s)\n", e.ID, e.Topic, e.Visibility)
			fmt.Println(e.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&topic, "topic", "", "topic (required)")
	f.StringVar(&content, "content", "", "entry content (default: read stdin)")
	f.StringVar(&visibility, "visibility", "org", "org | private")
	f.StringArrayVar(&tags, "tag", nil, "tag (repeatable)")
	return cmd
}

func newSearchCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Semantic search over knowledge",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			hits, err := commonplace.Search(cmd.Context(), c, args[0], topK)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(hits)
			}
			if len(hits) == 0 {
				fmt.Fprintln(os.Stderr, "no results")
				return nil
			}
			for _, h := range hits {
				fmt.Printf("%.3f  %-24s %s\n", h.Score, h.Entry.Topic, snippet(h.Entry.Content))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", 5, "max results")
	return cmd
}

func newListCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List knowledge entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			entries, err := commonplace.List(cmd.Context(), c)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(entries)
			}
			for _, e := range entries {
				fmt.Printf("%-22s %-8s %s\n", e.ID, e.Visibility, e.Topic)
			}
			return nil
		},
	}
	return cmd
}

func newUpdateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var topic, content, visibility string
	var tags []string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a knowledge entry (only the flags you set; --tag replaces all tags)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			var in commonplace.UpdateInput
			if f.Changed("topic") {
				in.Topic = &topic
			}
			if f.Changed("content") {
				in.Content = &content
			}
			if f.Changed("visibility") {
				in.Visibility = &visibility
			}
			if f.Changed("tag") {
				in.Tags = &tags
			}
			if in.Topic == nil && in.Content == nil && in.Visibility == nil && in.Tags == nil {
				return fmt.Errorf("nothing to update — set --topic/--content/--visibility/--tag")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			e, err := commonplace.Update(cmd.Context(), c, args[0], in)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(e)
			}
			fmt.Fprintf(os.Stderr, "updated %s (topic %q, %s)\n", e.ID, e.Topic, e.Visibility)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&topic, "topic", "", "new topic")
	f.StringVar(&content, "content", "", "new content")
	f.StringVar(&visibility, "visibility", "", "org | private")
	f.StringArrayVar(&tags, "tag", nil, "replace the entry's tags (repeatable)")
	return cmd
}

func newDeleteCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id> --yes",
		Short: "Delete a knowledge entry (irreversible; requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("pass --yes to confirm deletion (irreversible)")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			if err := commonplace.Delete(cmd.Context(), c, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "deleted %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the irreversible delete")
	return cmd
}

// snippet collapses content to a single short line for table output, truncating
// on rune boundaries so multi-byte characters are never sliced in half.
func snippet(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:57]) + "…"
	}
	return s
}
