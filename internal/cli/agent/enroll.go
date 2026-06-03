package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	casket "github.com/CarriedWorldUniverse/casket-go"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cwb-client/herald"
	"github.com/CarriedWorldUniverse/cwb-client/identity"
	"github.com/spf13/cobra"
)

// bootstrapKeyfile is the herald-rooted bootstrap keyfile the aspect runtime
// reads (nexus runtime/heraldkeyfile.Keyfile). Tags MUST match that struct.
type bootstrapKeyfile struct {
	Key         string `json:"key"`         // base64 ed25519 private key (64-byte Go form)
	KeyID       string `json:"key_id"`      // herald agent UUID
	URL         string `json:"url"`         // nexus relay the aspect connects/discovers through
	Slug        string `json:"slug"`        // agent name
	Fingerprint string `json:"fingerprint"` // base64url sha256(pub)[:16]
}

func newEnrollCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var slug, relayURL, out string
	cmd := &cobra.Command{
		Use:   "enroll --slug <slug> --url <nexus-relay> [--out <path>]",
		Short: "Write a bootstrap keyfile for an already-provisioned agent (attach)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" || relayURL == "" {
				return fmt.Errorf("--slug and --url are required")
			}
			seed := os.Getenv("CW_OWNER_SEED")
			if seed == "" {
				return fmt.Errorf("agent enroll requires the owner seed in CW_OWNER_SEED")
			}
			priv, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
			if err != nil {
				return fmt.Errorf("derive agent key: %w", err)
			}
			fp := identity.Fingerprint(pub)

			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			a, err := herald.GetAgentByFingerprint(cmd.Context(), c, fp)
			if err != nil {
				return fmt.Errorf("no agent for slug %q (fingerprint %s) at the edge — provision it first (or check the slug for a typo): %w", slug, fp, err)
			}

			kf := bootstrapKeyfile{
				Key:         base64.StdEncoding.EncodeToString(priv),
				KeyID:       a.ID,
				URL:         relayURL,
				Slug:        slug,
				Fingerprint: fp,
			}
			data, err := json.MarshalIndent(kf, "", "  ")
			if err != nil {
				return err
			}
			if out == "" {
				out = slug + ".keyfile.json"
			}
			if err := os.WriteFile(out, data, 0o600); err != nil {
				return fmt.Errorf("write keyfile %s: %w", out, err)
			}
			if err := os.Chmod(out, 0o600); err != nil {
				return fmt.Errorf("secure keyfile %s: %w", out, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", out)
			fmt.Fprintf(os.Stderr, "enrolled agent %s (%s); start the aspect with NEXUS_HERALD_KEYFILE=%s\n", a.ID, slug, out)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&slug, "slug", "", "casket key slug / agent name (required)")
	f.StringVar(&relayURL, "url", "", "nexus relay url the aspect connects/discovers through (required)")
	f.StringVar(&out, "out", "", "keyfile output path (default ./<slug>.keyfile.json)")
	return cmd
}
