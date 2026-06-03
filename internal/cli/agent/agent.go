// Package agent implements `cw agent`: keygen (mint an owner seed) and create
// (derive an agent's casket pubkey from CW_OWNER_SEED + slug, register it with
// herald). The derivation matches `cw auth login --agent`, so a created agent
// can immediately log in.
package agent

import (
	"crypto/rand"
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

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Provision agent identities (herald admin)"}
	cmd.AddCommand(newKeygenCmd(), newCreateCmd(gf), newPubkeyCmd(gf), newEnrollCmd(gf))
	return cmd
}

func newPubkeyCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "pubkey --slug <slug>",
		Short: "Derive an agent's casket pubkey + fingerprint from CW_OWNER_SEED (offline; no herald call)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" {
				return fmt.Errorf("--slug is required")
			}
			seed := os.Getenv("CW_OWNER_SEED")
			if seed == "" {
				return fmt.Errorf("agent pubkey requires the owner seed in CW_OWNER_SEED")
			}
			_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
			if err != nil {
				return fmt.Errorf("derive agent key: %w", err)
			}
			pubB64 := base64.StdEncoding.EncodeToString(pub)
			fp := identity.Fingerprint(pub)
			out := cmd.OutOrStdout()
			if gf.JSON {
				return json.NewEncoder(out).Encode(struct {
					Slug        string `json:"slug"`
					Pubkey      string `json:"pubkey"`
					Fingerprint string `json:"fingerprint"`
				}{slug, pubB64, fp})
			}
			fmt.Fprintf(out, "pubkey:      %s\nfingerprint: %s\n", pubB64, fp)
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "casket key slug (required)")
	return cmd
}

func newKeygenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keygen",
		Short: "Generate a fresh owner seed (set it as CW_OWNER_SEED)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return fmt.Errorf("generate seed: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), base64.StdEncoding.EncodeToString(b))
			fmt.Fprintln(os.Stderr, "set this as CW_OWNER_SEED; agents under it are distinguished by --slug")
			return nil
		},
	}
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var org, name, slug, responsibleHuman string
	var scopes []string
	cmd := &cobra.Command{
		Use:   "create --org <org> --name <dn> --slug <slug> --responsible-human <human-id>",
		Short: "Create an agent (derives its key from CW_OWNER_SEED + slug)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if org == "" || name == "" || slug == "" || responsibleHuman == "" {
				return fmt.Errorf("--org, --name, --slug, --responsible-human are required")
			}
			seed := os.Getenv("CW_OWNER_SEED")
			if seed == "" {
				return fmt.Errorf("agent create requires the owner seed in CW_OWNER_SEED")
			}
			_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
			if err != nil {
				return fmt.Errorf("derive agent key: %w", err)
			}
			pubB64 := base64.StdEncoding.EncodeToString(pub)

			c, sess, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			a, err := herald.CreateAgent(cmd.Context(), c, org, herald.CreateAgentInput{
				DisplayName: name, ResponsibleHuman: responsibleHuman, CasketPubkey: pubB64, Scopes: scopes,
			})
			if err != nil {
				return err
			}
			if gf.JSON {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(a); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), a.ID)
			}
			fmt.Fprintf(os.Stderr, "created agent %s (%s) in org %s; responsible human %s\n", a.ID, slug, a.Org, responsibleHuman)
			fmt.Fprintln(os.Stderr, "log in as it with:")
			fmt.Fprintf(os.Stderr, "  CW_OWNER_SEED=<your owner seed> CW_AGENT_ID=%s CW_AGENT_SLUG=%s cw auth login --agent --edge %s\n", a.ID, slug, sess.Edge)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&org, "org", "", "org id (required)")
	f.StringVar(&name, "name", "", "display name (required)")
	f.StringVar(&slug, "slug", "", "casket key slug, e.g. builder (required)")
	f.StringVar(&responsibleHuman, "responsible-human", "", "responsible human id (required)")
	f.StringArrayVar(&scopes, "scope", nil, "scope to grant, e.g. repo:read (repeatable)")
	return cmd
}
