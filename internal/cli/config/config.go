// Package config is the cw command group for raw almanac configuration:
// get / set / list arbitrary config paths. It exists so there is ONE supported
// way to change platform config (almanac is the source of truth) — no raw
// libSQL edits, no drift.
//
// Transport: the in-mesh mTLS gRPC path (CW_APP_TLS_CERT/_KEY/_CA, a cwb-ca
// client cert), the same break-glass transport `cw app` uses. Org/subject come
// from --org (or CW_APP_ORG) and CW_APP_SUBJECT. The interchange-edge transport
// (session bearer) is a deliberate follow-up; this path works without a live
// session, which is the point.
package config

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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

func dialAlmanac() (*grpc.ClientConn, error) {
	certPath := os.Getenv("CW_APP_TLS_CERT")
	keyPath := os.Getenv("CW_APP_TLS_KEY")
	caPath := os.Getenv("CW_APP_TLS_CA")
	if certPath == "" || keyPath == "" || caPath == "" {
		return nil, fmt.Errorf("cw config needs the in-mesh mTLS transport: set CW_APP_TLS_CERT/_KEY/_CA (a cwb-ca client cert)")
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
	addr := envOr("CW_APP_ALMANAC_ADDR", "almanac.cwb.svc.cluster.local:8083")
	return grpc.NewClient(addr, grpc.WithTransportCredentials(
		credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS13})))
}

func mdCtx(ctx context.Context, org, scopes string) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		"cwb-subject", envOr("CW_APP_SUBJECT", "croft"),
		"cwb-org", org,
		"cwb-scopes", scopes)
}

// New builds the `cw config` command group.
func New(_ *cmdutil.GlobalFlags) *cobra.Command {
	var org string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get/set/list raw almanac config (in-mesh mTLS; almanac is the source of truth)",
		Long: "One supported way to change platform configuration — almanac is authoritative.\n" +
			"Uses the in-mesh mTLS transport (CW_APP_TLS_CERT/_KEY/_CA). Org from --org or\n" +
			"CW_APP_ORG; subject from CW_APP_SUBJECT (default croft).",
	}
	cmd.PersistentFlags().StringVar(&org, "org", envOr("CW_APP_ORG", "carriedworld"), "almanac org")

	get := &cobra.Command{
		Use:   "get <path>",
		Short: "Print a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			conn, err := dialAlmanac()
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := cwbv1.NewConfigServiceClient(conn).GetConfig(
				mdCtx(c.Context(), org, "config:read"),
				&cwbv1.GetConfigRequest{Path: a[0]})
			if err != nil {
				return err
			}
			v := resp.GetItem().GetValue()
			fmt.Print(v)
			if !strings.HasSuffix(v, "\n") {
				fmt.Println()
			}
			return nil
		},
	}

	var fromFile string
	set := &cobra.Command{
		Use:   "set <path>",
		Short: "Set a config value (--from-file FILE, or stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			var val []byte
			var err error
			if fromFile != "" {
				val, err = os.ReadFile(fromFile)
			} else {
				val, err = io.ReadAll(os.Stdin)
			}
			if err != nil {
				return err
			}
			conn, err := dialAlmanac()
			if err != nil {
				return err
			}
			defer conn.Close()
			_, err = cwbv1.NewConfigServiceClient(conn).SetConfig(
				mdCtx(c.Context(), org, "config:write"),
				&cwbv1.SetConfigRequest{Path: a[0], Value: string(val)})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "set %s (org %s, %d bytes)\n", a[0], org, len(val))
			return nil
		},
	}
	set.Flags().StringVar(&fromFile, "from-file", "", "read the value from this file (default: stdin)")

	list := &cobra.Command{
		Use:   "list [prefix]",
		Short: "List config paths (optionally under a prefix)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			prefix := ""
			if len(a) == 1 {
				prefix = a[0]
			}
			conn, err := dialAlmanac()
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := cwbv1.NewConfigServiceClient(conn).ListConfig(
				mdCtx(c.Context(), org, "config:read"),
				&cwbv1.ListConfigRequest{Prefix: prefix})
			if err != nil {
				return err
			}
			for _, it := range resp.GetItems() {
				fmt.Printf("%s\tv%d\n", it.GetPath(), it.GetVersion())
			}
			return nil
		},
	}

	cmd.AddCommand(get, set, list)
	return cmd
}
