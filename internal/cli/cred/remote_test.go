package cred

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeCredentialService is an in-process stub of cwbv1.CredentialServiceServer.
// Each field defaults to "unimplemented" behaviour via the embedded type;
// tests set only the methods they exercise.
type fakeCredentialService struct {
	cwbv1.UnimplementedCredentialServiceServer
	fetch  func(context.Context, *cwbv1.FetchRequest) (*cwbv1.FetchResponse, error)
	set    func(context.Context, *cwbv1.SetCredentialRequest) (*cwbv1.SetCredentialResponse, error)
	list   func(context.Context, *cwbv1.ListCredentialsRequest) (*cwbv1.ListCredentialsResponse, error)
	delete func(context.Context, *cwbv1.DeleteCredentialRequest) (*cwbv1.DeleteCredentialResponse, error)
}

func (f *fakeCredentialService) Fetch(ctx context.Context, r *cwbv1.FetchRequest) (*cwbv1.FetchResponse, error) {
	return f.fetch(ctx, r)
}

func (f *fakeCredentialService) SetCredential(ctx context.Context, r *cwbv1.SetCredentialRequest) (*cwbv1.SetCredentialResponse, error) {
	return f.set(ctx, r)
}

func (f *fakeCredentialService) ListCredentials(ctx context.Context, r *cwbv1.ListCredentialsRequest) (*cwbv1.ListCredentialsResponse, error) {
	return f.list(ctx, r)
}

func (f *fakeCredentialService) DeleteCredential(ctx context.Context, r *cwbv1.DeleteCredentialRequest) (*cwbv1.DeleteCredentialResponse, error) {
	return f.delete(ctx, r)
}

// startFakeCustodian dials an in-process CredentialService stub over plain
// insecure bufconn and overrides credentialClient for the duration of the
// test. Wiring a full mesh mTLS cert chain through bufconn is disproportionate
// for unit coverage of the CLI logic — the real mTLS dial path (dialCustodian)
// is exercised separately by TestRemoteNeedsMeshCert, which never gets past
// the pre-dial cert check.
func startFakeCustodian(t *testing.T, srv cwbv1.CredentialServiceServer) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	cwbv1.RegisterCredentialServiceServer(s, srv)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := cwbv1.NewCredentialServiceClient(conn)
	old := credentialClient
	credentialClient = func() (cwbv1.CredentialServiceClient, func(), error) {
		return client, func() {}, nil
	}
	t.Cleanup(func() { credentialClient = old })
}

func mdFrom(ctx context.Context, key string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vs := md.Get(key)
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

func TestRemoteGetSecretPrintsValueOnly(t *testing.T) {
	startFakeCustodian(t, &fakeCredentialService{
		fetch: func(ctx context.Context, r *cwbv1.FetchRequest) (*cwbv1.FetchResponse, error) {
			if r.GetKind() != "secret" || r.GetName() != "meshy-api-key" {
				t.Fatalf("unexpected request: %+v", r)
			}
			if mdFrom(ctx, "cwb-org") != "cwb" || mdFrom(ctx, "cwb-scopes") != "cred:read" {
				t.Fatalf("unexpected metadata: org=%q scopes=%q", mdFrom(ctx, "cwb-org"), mdFrom(ctx, "cwb-scopes"))
			}
			return &cwbv1.FetchResponse{Kind: "secret", Name: "meshy-api-key",
				Bundle: &cwbv1.FetchResponse_SecretBundle{SecretBundle: &cwbv1.SecretBundle{Value: "shh-secret"}}}, nil
		},
	})

	var out bytes.Buffer
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"get", "cwb/meshy-api-key"})
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.String() != "shh-secret" {
		t.Fatalf("stdout = %q, want raw secret value with no decoration", out.String())
	}
}

func TestRemotePutSendsSecretBundleWithHints(t *testing.T) {
	var got *cwbv1.SetCredentialRequest
	startFakeCustodian(t, &fakeCredentialService{
		set: func(ctx context.Context, r *cwbv1.SetCredentialRequest) (*cwbv1.SetCredentialResponse, error) {
			got = r
			if mdFrom(ctx, "cwb-scopes") != "cred:write" {
				t.Fatalf("scopes = %q", mdFrom(ctx, "cwb-scopes"))
			}
			return &cwbv1.SetCredentialResponse{Item: &cwbv1.CredentialMeta{Kind: r.GetKind(), Name: r.GetName()}}, nil
		},
	})

	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"put", "cwb/meshy-api-key", "--host", "api.example.com", "--username", "x-api-key"})
	cmd.SetIn(strings.NewReader("shh-secret"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got == nil {
		t.Fatal("SetCredential not called")
	}
	if got.GetKind() != "secret" || got.GetName() != "meshy-api-key" {
		t.Fatalf("kind/name = %q/%q", got.GetKind(), got.GetName())
	}
	b := got.GetSecretBundle()
	if b.GetValue() != "shh-secret" || b.GetHost() != "api.example.com" || b.GetUsername() != "x-api-key" {
		t.Fatalf("bundle = %+v", b)
	}
}

func TestRemotePutRejectsNonSecretKind(t *testing.T) {
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"put", "cwb/meshy-api-key", "--kind", "git"})
	cmd.SetIn(strings.NewReader("shh-secret"))
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "only supports --kind secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteListPrintsKindSlashName(t *testing.T) {
	startFakeCustodian(t, &fakeCredentialService{
		list: func(ctx context.Context, r *cwbv1.ListCredentialsRequest) (*cwbv1.ListCredentialsResponse, error) {
			return &cwbv1.ListCredentialsResponse{Items: []*cwbv1.CredentialMeta{
				{Kind: "secret", Name: "meshy-api-key"},
				{Kind: "git", Name: "github.com"},
			}}, nil
		},
	})

	var out bytes.Buffer
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"ls", "cwb"})
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	want := "secret/meshy-api-key\ngit/github.com\n"
	if out.String() != want {
		t.Fatalf("ls = %q, want %q", out.String(), want)
	}
}

func TestRemoteRmDeletedTrueIsSilent(t *testing.T) {
	startFakeCustodian(t, &fakeCredentialService{
		delete: func(ctx context.Context, r *cwbv1.DeleteCredentialRequest) (*cwbv1.DeleteCredentialResponse, error) {
			return &cwbv1.DeleteCredentialResponse{Deleted: true}, nil
		},
	})

	var out bytes.Buffer
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"rm", "cwb/meshy-api-key"})
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("rm output = %q, want silent success", out.String())
	}
}

func TestRemoteRmDeletedFalseIsNotFound(t *testing.T) {
	startFakeCustodian(t, &fakeCredentialService{
		delete: func(ctx context.Context, r *cwbv1.DeleteCredentialRequest) (*cwbv1.DeleteCredentialResponse, error) {
			return &cwbv1.DeleteCredentialResponse{Deleted: false}, nil
		},
	})

	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"rm", "cwb/missing"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cwb/missing: not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemotePermissionDeniedSurfacesForbidden(t *testing.T) {
	startFakeCustodian(t, &fakeCredentialService{
		fetch: func(ctx context.Context, r *cwbv1.FetchRequest) (*cwbv1.FetchResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "caller lacks cred:write")
		},
	})

	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"get", "cwb/meshy-api-key"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("get should fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "forbidden") || !strings.Contains(msg, "lacks cred:read") {
		t.Fatalf("error = %q", msg)
	}
}

func TestRemoteNeedsMeshCert(t *testing.T) {
	for _, k := range []string{"CW_APP_TLS_CERT", "CW_APP_TLS_KEY", "CW_APP_TLS_CA"} {
		t.Setenv(k, "")
	}
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"get", "cwb/meshy-api-key"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "in-mesh mTLS") {
		t.Fatalf("get without mesh cert should give the mTLS-transport hint; got %v", err)
	}
}
