// Package app is the cw command group for Strata app declarations: declare/rm
// write almanac (/cwb/mason/apps/<name>); ls/status/sync read mason.
//
// M1 transport is direct in-cluster mTLS (CW_APP_* env); the interchange edge
// path arrives with NEX-621.
package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	sigsyaml "sigs.k8s.io/yaml"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Declare and inspect Strata apps (mason)",
		Long: "Declarations are written to almanac (/cwb/mason/apps/<name>); mason reconciles\n" +
			"them onto the cluster. M1 transport is direct in-cluster mTLS via CW_APP_* env;\n" +
			"the interchange edge path arrives with NEX-621.",
	}
	cmd.AddCommand(newDeclare(), newLs(), newStatus(), newRm(), newSync())
	return cmd
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func dial(addr string) (*grpc.ClientConn, error) {
	certPath := os.Getenv("CW_APP_TLS_CERT")
	keyPath := os.Getenv("CW_APP_TLS_KEY")
	caPath := os.Getenv("CW_APP_TLS_CA")
	if certPath == "" || keyPath == "" || caPath == "" {
		return nil, fmt.Errorf("cw app needs CW_APP_TLS_CERT/_KEY/_CA (cwb-ca client cert; M1 in-cluster transport)")
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
	return grpc.NewClient(addr, grpc.WithTransportCredentials(
		credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS13})))
}

func mdCtx(ctx context.Context, scopes string) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		"cwb-subject", envOr("CW_APP_SUBJECT", "croft"),
		"cwb-org", envOr("CW_APP_ORG", "carriedworld"),
		"cwb-scopes", scopes)
}

func declPath(name string) string { return envOr("CW_APP_PREFIX", "cwb/mason/apps/") + name }

func precheck(name string, y []byte) error {
	var d struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Image     string `json:"image"`
	}
	if err := sigsyaml.Unmarshal(y, &d); err != nil {
		return fmt.Errorf("parse declaration: %w", err)
	}
	if d.Name == "" || d.Namespace == "" || d.Image == "" {
		return fmt.Errorf("declaration needs name, namespace, image")
	}
	if d.Name != name {
		return fmt.Errorf("declaration name %q must match %q", d.Name, name)
	}
	return nil
}
