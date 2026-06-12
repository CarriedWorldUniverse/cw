// Package app is the cw command group for Strata app declarations: declare/rm
// write almanac (cwb/mason/apps/<name>); ls/status/sync read mason.
//
// Default transport is the interchange edge (REST + session bearer, like every
// other cw group). Setting CW_APP_TLS_CERT opts into the direct in-mesh gRPC
// transport (break-glass; see grpc.go).
package app

import (
	"context"
	"fmt"
	"os"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/spf13/cobra"
	sigsyaml "sigs.k8s.io/yaml"
)

func New(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Declare and inspect Strata apps (mason)",
		Long: "Declarations are written to almanac (cwb/mason/apps/<name>); mason reconciles\n" +
			"them onto the cluster. Commands go through the interchange edge with the\n" +
			"session bearer (same auth as the rest of cw). Setting CW_APP_TLS_CERT/_KEY/_CA\n" +
			"switches to the direct in-mesh mTLS transport (break-glass when the edge is\n" +
			"down; needs CW_APP_MASON_ADDR/CW_APP_ALMANAC_ADDR reachability).",
	}
	cmd.AddCommand(newDeclare(gf), newLs(gf), newStatus(gf), newRm(gf), newSync(gf))
	return cmd
}

// appStatus is one app's reconcile state, in the gateway's snake_case JSON
// shape (UseProtoNames; phase arrives as the enum name, e.g. APP_PHASE_SYNCED).
type appStatus struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Phase       string `json:"phase"`
	Message     string `json:"message"`
	Ready       string `json:"ready"`
	DeclHash    string `json:"decl_hash"`
	AppliedHash string `json:"applied_hash"`
	LastApplied string `json:"last_applied_at"`
	LastChecked string `json:"last_checked_at"`
}

// appAPI is the transport seam: edgeAPI (default) or grpcAPI (CW_APP_TLS_CERT).
type appAPI interface {
	listApps(ctx context.Context) ([]appStatus, error)
	getApp(ctx context.Context, name string) (appStatus, string, error)
	triggerSync(ctx context.Context, name string) ([]appStatus, error)
	declare(ctx context.Context, name, yaml string) error
	remove(ctx context.Context, name string) error
}

// newAPI selects the transport: CW_APP_TLS_CERT set → direct in-mesh gRPC;
// otherwise the interchange edge with the session bearer.
func newAPI(gf *cmdutil.GlobalFlags) (appAPI, error) {
	if os.Getenv("CW_APP_TLS_CERT") != "" {
		return grpcAPI{}, nil
	}
	c, _, _, err := cmdutil.Session(gf)
	if err != nil {
		return nil, err
	}
	return edgeAPI{c: c}, nil
}

// phaseDisplay maps an AppPhase enum name to its display form; unrecognized
// names pass through verbatim so new phases stay visible.
func phaseDisplay(p string) string {
	switch p {
	case "APP_PHASE_SYNCED":
		return "Synced"
	case "APP_PHASE_PROGRESSING":
		return "Progressing"
	case "APP_PHASE_DEGRADED":
		return "Degraded"
	case "APP_PHASE_INVALID":
		return "Invalid"
	case "APP_PHASE_UNKNOWN", "APP_PHASE_UNSPECIFIED", "":
		return "Unknown"
	default:
		return p
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
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
