// Package kb implements `cw kb`: store, search, list, update, delete over
// commonplace knowledge.
//
// Transport: the in-mesh mTLS gRPC path (CW_APP_TLS_CERT/_KEY/_CA, a cwb-ca
// client cert) straight to commonplace's KnowledgeService — the same sovereign
// transport `cw config` uses for almanac. Org/subject come from --org (or
// CW_APP_ORG) and CW_APP_SUBJECT. This works without a live herald/edge session,
// which is the point: knowledge is operable from croft with just the mesh cert.
package kb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// dialCommonplace builds the in-mesh mTLS connection to commonplace's
// KnowledgeService, mirroring cw config's dialAlmanac.
func dialCommonplace() (*grpc.ClientConn, error) {
	certPath := os.Getenv("CW_APP_TLS_CERT")
	keyPath := os.Getenv("CW_APP_TLS_KEY")
	caPath := os.Getenv("CW_APP_TLS_CA")
	if certPath == "" || keyPath == "" || caPath == "" {
		return nil, fmt.Errorf("cw kb needs the in-mesh mTLS transport: set CW_APP_TLS_CERT/_KEY/_CA (a cwb-ca client cert)")
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("CW_APP_TLS_CERT/_KEY: %w", err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("CW_APP_TLS_CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("CW_APP_TLS_CA: no certs parsed")
	}
	addr := envOr("CW_APP_COMMONPLACE_ADDR", "commonplace.cwb.svc.cluster.local:8101")
	return grpc.NewClient(addr, grpc.WithTransportCredentials(
		credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS13})))
}

func mdCtx(ctx context.Context, org, scopes string) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		"cwb-subject", envOr("CW_APP_SUBJECT", "croft"),
		"cwb-org", org,
		"cwb-scopes", scopes)
}

// client dials commonplace and returns the KnowledgeService client plus a
// cleanup func; callers defer the cleanup.
func knowledgeClient() (cwbv1.KnowledgeServiceClient, func(), error) {
	conn, err := dialCommonplace()
	if err != nil {
		return nil, nil, err
	}
	return cwbv1.NewKnowledgeServiceClient(conn), func() { _ = conn.Close() }, nil
}

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var org string
	cmd := &cobra.Command{
		Use:   "kb",
		Short: "Manage commonplace knowledge (in-mesh mTLS)",
		Long: "Store/search/list/update/delete commonplace knowledge.\n" +
			"Uses the in-mesh mTLS transport (CW_APP_TLS_CERT/_KEY/_CA). Org from --org or\n" +
			"CW_APP_ORG; subject from CW_APP_SUBJECT (default croft).",
	}
	cmd.PersistentFlags().StringVar(&org, "org", envOr("CW_APP_ORG", "carriedworld"), "commonplace org")
	cmd.AddCommand(
		newStoreCmd(gf, &org),
		newSearchCmd(gf, &org),
		newListCmd(gf, &org),
		newUpdateCmd(gf, &org),
		newDeleteCmd(gf, &org),
	)
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

func newStoreCmd(gf *cmdutil.GlobalFlags, org *string) *cobra.Command {
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
			kc, done, err := knowledgeClient()
			if err != nil {
				return err
			}
			defer done()
			resp, err := kc.Store(mdCtx(cmd.Context(), *org, "knowledge:write"),
				&cwbv1.StoreRequest{Topic: topic, Content: body, Visibility: visibility, Tags: tags})
			if err != nil {
				return err
			}
			e := resp.GetEntry()
			fmt.Fprintf(os.Stderr, "stored %s (topic %q, %s)\n", e.GetId(), e.GetTopic(), e.GetVisibility())
			fmt.Println(e.GetId())
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

func newSearchCmd(gf *cmdutil.GlobalFlags, org *string) *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Semantic search over knowledge",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kc, done, err := knowledgeClient()
			if err != nil {
				return err
			}
			defer done()
			resp, err := kc.Search(mdCtx(cmd.Context(), *org, "knowledge:read"),
				&cwbv1.SearchRequest{Q: args[0], TopK: int32(topK)})
			if err != nil {
				return err
			}
			hits := resp.GetHits()
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(hits)
			}
			if len(hits) == 0 {
				fmt.Fprintln(os.Stderr, "no results")
				return nil
			}
			for _, h := range hits {
				fmt.Printf("%.3f  %-24s %s\n", h.GetScore(), h.GetEntry().GetTopic(), snippet(h.GetEntry().GetContent()))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", 5, "max results")
	return cmd
}

func newListCmd(gf *cmdutil.GlobalFlags, org *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List knowledge entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			kc, done, err := knowledgeClient()
			if err != nil {
				return err
			}
			defer done()
			resp, err := kc.List(mdCtx(cmd.Context(), *org, "knowledge:read"), &cwbv1.ListRequest{})
			if err != nil {
				return err
			}
			entries := resp.GetEntries()
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(entries)
			}
			for _, e := range entries {
				fmt.Printf("%-22s %-8s %s\n", e.GetId(), e.GetVisibility(), e.GetTopic())
			}
			return nil
		},
	}
	return cmd
}

func newUpdateCmd(gf *cmdutil.GlobalFlags, org *string) *cobra.Command {
	var topic, content, visibility string
	var tags []string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a knowledge entry (only the flags you set; --tag replaces all tags)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			req := &cwbv1.UpdateRequest{Id: args[0]}
			any := false
			if f.Changed("topic") {
				req.Topic = topic
				any = true
			}
			if f.Changed("content") {
				req.Content = content
				any = true
			}
			if f.Changed("visibility") {
				req.Visibility = visibility
				any = true
			}
			if f.Changed("tag") {
				req.Tags = tags
				any = true
			}
			if !any {
				return fmt.Errorf("nothing to update — set --topic/--content/--visibility/--tag")
			}
			kc, done, err := knowledgeClient()
			if err != nil {
				return err
			}
			defer done()
			resp, err := kc.Update(mdCtx(cmd.Context(), *org, "knowledge:write"), req)
			if err != nil {
				return err
			}
			e := resp.GetEntry()
			if gf.JSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(e)
			}
			fmt.Fprintf(os.Stderr, "updated %s (topic %q, %s)\n", e.GetId(), e.GetTopic(), e.GetVisibility())
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

func newDeleteCmd(gf *cmdutil.GlobalFlags, org *string) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id> --yes",
		Short: "Delete a knowledge entry (irreversible; requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("pass --yes to confirm deletion (irreversible)")
			}
			kc, done, err := knowledgeClient()
			if err != nil {
				return err
			}
			defer done()
			if _, err := kc.Delete(mdCtx(cmd.Context(), *org, "knowledge:write"), &cwbv1.DeleteRequest{Id: args[0]}); err != nil {
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
