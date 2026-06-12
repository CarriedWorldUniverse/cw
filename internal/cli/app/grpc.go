package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// grpcAPI is the in-mesh break-glass transport: direct mTLS gRPC to mason and
// almanac with a cwb-ca client cert, selected by setting CW_APP_TLS_CERT
// (plus _KEY/_CA). Identity comes from CW_APP_SUBJECT/CW_APP_ORG metadata —
// only usable from inside the mesh (or a port-forward) where the pillar
// addresses resolve. The normal path is the interchange edge (edge.go).
type grpcAPI struct{}

func dial(addr string) (*grpc.ClientConn, error) {
	certPath := os.Getenv("CW_APP_TLS_CERT")
	keyPath := os.Getenv("CW_APP_TLS_KEY")
	caPath := os.Getenv("CW_APP_TLS_CA")
	if certPath == "" || keyPath == "" || caPath == "" {
		return nil, fmt.Errorf("direct transport needs all of CW_APP_TLS_CERT/_KEY/_CA (cwb-ca client cert); unset CW_APP_TLS_CERT to use the edge")
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

func dialMason() (*grpc.ClientConn, error) {
	return dial(envOr("CW_APP_MASON_ADDR", "mason.cwb.svc.cluster.local:8086"))
}

func dialAlmanac() (*grpc.ClientConn, error) {
	return dial(envOr("CW_APP_ALMANAC_ADDR", "almanac.cwb.svc.cluster.local:8083"))
}

func fromProto(a *cwbv1.AppStatus) appStatus {
	return appStatus{
		Name:        a.GetName(),
		Namespace:   a.GetNamespace(),
		Phase:       a.GetPhase().String(),
		Message:     a.GetMessage(),
		Ready:       a.GetReady(),
		DeclHash:    a.GetDeclHash(),
		AppliedHash: a.GetAppliedHash(),
		LastApplied: a.GetLastAppliedAt(),
		LastChecked: a.GetLastCheckedAt(),
	}
}

func fromProtoAll(in []*cwbv1.AppStatus) []appStatus {
	out := make([]appStatus, 0, len(in))
	for _, a := range in {
		out = append(out, fromProto(a))
	}
	return out
}

func (grpcAPI) listApps(ctx context.Context) ([]appStatus, error) {
	conn, err := dialMason()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	resp, err := cwbv1.NewAppServiceClient(conn).ListApps(mdCtx(ctx, "app:read"), &cwbv1.ListAppsRequest{})
	if err != nil {
		return nil, err
	}
	return fromProtoAll(resp.GetApps()), nil
}

func (grpcAPI) getApp(ctx context.Context, name string) (appStatus, string, error) {
	conn, err := dialMason()
	if err != nil {
		return appStatus{}, "", err
	}
	defer conn.Close()
	resp, err := cwbv1.NewAppServiceClient(conn).GetApp(mdCtx(ctx, "app:read"), &cwbv1.GetAppRequest{Name: name})
	if err != nil {
		return appStatus{}, "", err
	}
	return fromProto(resp.GetApp()), resp.GetDeclaration(), nil
}

func (grpcAPI) triggerSync(ctx context.Context, name string) ([]appStatus, error) {
	conn, err := dialMason()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	resp, err := cwbv1.NewAppServiceClient(conn).TriggerSync(mdCtx(ctx, "app:write"), &cwbv1.TriggerSyncRequest{Name: name})
	if err != nil {
		return nil, err
	}
	return fromProtoAll(resp.GetApps()), nil
}

func (grpcAPI) declare(ctx context.Context, name, yaml string) error {
	conn, err := dialAlmanac()
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = cwbv1.NewConfigServiceClient(conn).SetConfig(
		mdCtx(ctx, "config:write"),
		&cwbv1.SetConfigRequest{Path: declPath(name), Value: yaml})
	return err
}

func (grpcAPI) remove(ctx context.Context, name string) error {
	conn, err := dialAlmanac()
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = cwbv1.NewConfigServiceClient(conn).DeleteConfig(
		mdCtx(ctx, "config:write"),
		&cwbv1.DeleteConfigRequest{Path: declPath(name)})
	return err
}
