package cred

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"os"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Remote transport: in-mesh mTLS gRPC straight to custodian's
// CredentialService — the same sovereign transport `cw kb` (commonplace) and
// `cw config` (almanac) use. This REPLACES the old interchange-edge HTTP
// remote-get path, which depended on a live herald session (dormant in the
// sovereign setup, so `<org>/<name>` was effectively unusable from croft).

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// dialCustodian builds the in-mesh mTLS connection to custodian's
// CredentialService, mirroring cw kb's dialCommonplace / cw config's
// dialAlmanac.
func dialCustodian() (*grpc.ClientConn, error) {
	certPath := os.Getenv("CW_APP_TLS_CERT")
	keyPath := os.Getenv("CW_APP_TLS_KEY")
	caPath := os.Getenv("CW_APP_TLS_CA")
	if certPath == "" || keyPath == "" || caPath == "" {
		return nil, fmt.Errorf("cw cred remote needs the in-mesh mTLS transport: set CW_APP_TLS_CERT/_KEY/_CA (a cwb-ca client cert)")
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
	addr := envOr("CW_APP_CUSTODIAN_ADDR", "custodian.cwb.svc.cluster.local:8085")
	return grpc.NewClient(addr, grpc.WithTransportCredentials(
		credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS13})))
}

func mdCtx(ctx context.Context, org, scopes string) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		"cwb-subject", envOr("CW_APP_SUBJECT", "croft"),
		"cwb-org", org,
		"cwb-scopes", scopes)
}

// credentialClient returns a CredentialServiceClient plus a cleanup func the
// caller must invoke when done. Production dials the in-mesh mTLS custodian
// transport above; tests override this var to point at an in-process stub
// server (see remote_test.go). Wiring a full mesh cert chain through bufconn
// for unit coverage is disproportionate, so the stub dials over plain
// insecure bufconn instead — that's a deliberate, noted deviation from
// "stub over bufconn with local TLS certs."
var credentialClient = func() (cwbv1.CredentialServiceClient, func(), error) {
	conn, err := dialCustodian()
	if err != nil {
		return nil, nil, err
	}
	return cwbv1.NewCredentialServiceClient(conn), func() { _ = conn.Close() }, nil
}

// remoteErr maps a gRPC error to cred's existing error wording, mirroring the
// style the old edge-HTTP path used for 403s. subject describes what ref
// names for the forbidden message — "that org's secret" for a single
// (kind, name) op, "that org's credentials" for a namespace-wide op like ls.
func remoteErr(op, ref, scope, subject string, err error) error {
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.PermissionDenied:
			return fmt.Errorf("cred: forbidden %s: caller lacks %s for %s: %s", ref, scope, subject, st.Message())
		case codes.NotFound:
			return fmt.Errorf("cred: no such secret %s", ref)
		}
	}
	return fmt.Errorf("cred: %s %s: %w", op, ref, err)
}

// remoteGet fetches (kind, name) in org and writes it to w. kind=="secret"
// writes the raw secret value with no trailing decoration; other kinds write
// the (non-secret-shaped) bundle as JSON, since they're structured.
func remoteGet(ctx context.Context, w io.Writer, org, name, kind string) error {
	ref := org + "/" + name
	cc, done, err := credentialClient()
	if err != nil {
		return err
	}
	defer done()
	resp, err := cc.Fetch(mdCtx(ctx, org, "cred:read"), &cwbv1.FetchRequest{Kind: kind, Name: name})
	if err != nil {
		return remoteErr("get", ref, "cred:read", "that org's secret", err)
	}
	switch kind {
	case "secret":
		_, err := io.WriteString(w, resp.GetSecretBundle().GetValue())
		return err
	case "git":
		return json.NewEncoder(w).Encode(resp.GetGitBundle())
	case "oauth":
		return json.NewEncoder(w).Encode(resp.GetOauthBundle())
	default:
		return fmt.Errorf("cred: get %s: unknown kind %q", ref, kind)
	}
}

// remotePut stores value as a SecretBundle for (kind, name) in org. Only
// kind=="secret" is supported for remote put in this release; git/oauth
// writes stay out of scope.
func remotePut(ctx context.Context, org, name, kind string, value []byte, host, username string) error {
	ref := org + "/" + name
	if kind != "secret" {
		return fmt.Errorf("cred: remote put only supports --kind secret in this release (git/oauth writes are out of scope): %s", ref)
	}
	cc, done, err := credentialClient()
	if err != nil {
		return err
	}
	defer done()
	_, err = cc.SetCredential(mdCtx(ctx, org, "cred:write"), &cwbv1.SetCredentialRequest{
		Kind: kind,
		Name: name,
		Bundle: &cwbv1.SetCredentialRequest_SecretBundle{SecretBundle: &cwbv1.SecretBundle{
			Value:    string(value),
			Host:     host,
			Username: username,
		}},
	})
	if err != nil {
		return remoteErr("put", ref, "cred:write", "that org's secret", err)
	}
	return nil
}

// remoteList lists all credentials (all kinds) in org, one "kind/name" per
// line — local ls prints bare names, but remote prefixes with kind since
// multiple kinds share the namespace.
func remoteList(ctx context.Context, w io.Writer, org string) error {
	cc, done, err := credentialClient()
	if err != nil {
		return err
	}
	defer done()
	resp, err := cc.ListCredentials(mdCtx(ctx, org, "cred:read"), &cwbv1.ListCredentialsRequest{})
	if err != nil {
		return remoteErr("ls", org, "cred:read", "that org's credentials", err)
	}
	for _, it := range resp.GetItems() {
		if _, err := fmt.Fprintf(w, "%s/%s\n", it.GetKind(), it.GetName()); err != nil {
			return err
		}
	}
	return nil
}

// remoteDelete deletes (kind, name) in org. deleted=false is reported as a
// clean not-found error (the RPC itself treats it as a non-error, idempotent
// outcome, but the CLI verb needs to fail so callers can tell "removed" from
// "already gone").
func remoteDelete(ctx context.Context, org, name, kind string) error {
	ref := org + "/" + name
	cc, done, err := credentialClient()
	if err != nil {
		return err
	}
	defer done()
	resp, err := cc.DeleteCredential(mdCtx(ctx, org, "cred:write"), &cwbv1.DeleteCredentialRequest{Kind: kind, Name: name})
	if err != nil {
		return remoteErr("rm", ref, "cred:write", "that org's secret", err)
	}
	if !resp.GetDeleted() {
		return fmt.Errorf("cred: %s: not found", ref)
	}
	return nil
}
