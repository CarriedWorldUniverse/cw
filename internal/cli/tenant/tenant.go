// Package tenant implements `cw tenant`: one-step onboarding of a customer org
// (platform-admin). `cw tenant onboard` composes the existing herald admin seams
// — create org + enable products + create the owner human with the org-owner
// role — into a single command, then prints where the owner logs in and gets
// the CLI.
package tenant

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/CarriedWorldUniverse/cwb-client/herald"
	"github.com/spf13/cobra"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// roleOrgOwner is the wire token herald expands (server-side) to the org-owner
// scope bundle. The bundle's scope LIST lives in herald (single source of
// truth); this is only the stable role name the onboard flow grants.
const roleOrgOwner = "role:org-owner"

// defaultCLIDownloadPath is appended to the console base when no explicit
// --cli-download-url is given — where the console's downloads page will serve
// the `cw` binaries (M3/M5 of the platform plan).
const defaultCLIDownloadPath = "/downloads"

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "tenant", Short: "Onboard and manage customer orgs (platform-admin)"}
	cmd.AddCommand(newOnboardCmd(gf), newOffboardCmd(gf))
	return cmd
}

// onboardResult is the structured summary (--json) of an onboarding.
type onboardResult struct {
	Org              string `json:"org"`
	OrgName          string `json:"org_name"`
	Owner            string `json:"owner"`
	OwnerEmail       string `json:"owner_email"`
	Role             string `json:"role"`
	PasswordSet      bool   `json:"password_set"`
	AlreadyOnboarded bool   `json:"already_onboarded"`
	ConsoleURL       string `json:"console_url"`
	CLIDownloadURL   string `json:"cli_download_url"`
	ProductsEnabled  string `json:"products_enabled"`
}

// findOrgByName returns the existing org with this name. ok is false when none
// exists; an error means more than one matches (ambiguous — onboard fails closed
// rather than guess which tenant, since herald does not enforce unique names).
func findOrgByName(orgs []herald.Org, name string) (herald.Org, bool, error) {
	var matches []herald.Org
	for _, o := range orgs {
		if o.Name == name {
			matches = append(matches, o)
		}
	}
	switch len(matches) {
	case 0:
		return herald.Org{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return herald.Org{}, false, fmt.Errorf("%d orgs named %q already exist — resolve with `cw org`/`cw human` directly", len(matches), name)
	}
}

func newOnboardCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var owner string
	var products []string
	var passwordStdin bool
	var consoleURL, cliDownloadURL string
	cmd := &cobra.Command{
		Use:   "onboard <org-name> --owner <email>",
		Short: "Create an isolated customer org with an org-owner (one step)",
		Long: "Onboard a customer org: create the org, enable its products (all " +
			"by default, or restrict with --product), create the owner human and " +
			"grant the org-owner role, then print the console URL and CLI download " +
			"link. The owner is org-scoped — herald refuses any platform-admin grant.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("org name is required")
			}
			if strings.TrimSpace(owner) == "" {
				return fmt.Errorf("--owner <email> is required")
			}
			// Read the optional initial password BEFORE any network call, so a
			// stdin problem fails fast and nothing is half-provisioned.
			var pw string
			if passwordStdin {
				var err error
				if pw, err = readPasswordStdin(cmd.InOrStdin()); err != nil {
					return err
				}
			}

			c, sessCtx, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			// Idempotence: if an org with this name already exists, reuse it and
			// provision nothing new (re-running onboard is a no-op).
			existingOrgs, err := herald.ListOrgs(ctx, c)
			if err != nil {
				return fmt.Errorf("list orgs: %w", err)
			}
			existing, found, err := findOrgByName(existingOrgs, name)
			if err != nil {
				return err
			}

			console := strings.TrimRight(firstNonEmpty(consoleURL, sessCtx.Edge), "/")
			download := cliDownloadURL
			if download == "" {
				download = console + defaultCLIDownloadPath
			}
			productsEnabled := "all"
			if len(products) > 0 {
				productsEnabled = strings.Join(products, ",")
			}

			if found {
				return printResult(cmd.OutOrStdout(), gf.JSON, onboardResult{
					Org:              existing.ID,
					OrgName:          existing.Name,
					OwnerEmail:       owner,
					Role:             roleOrgOwner,
					AlreadyOnboarded: true,
					ConsoleURL:       console,
					CLIDownloadURL:   download,
					ProductsEnabled:  productsEnabled,
				})
			}

			org, err := herald.CreateOrg(ctx, c, herald.CreateOrgInput{Name: name, Products: products})
			if err != nil {
				return fmt.Errorf("create org: %w", err)
			}
			human, err := herald.CreateHuman(ctx, c, org.ID, herald.CreateHumanInput{
				DisplayName: owner,
				Scopes:      []string{roleOrgOwner},
			})
			if err != nil {
				return fmt.Errorf("create owner (org %s created): %w", org.ID, err)
			}
			passwordSet := false
			if passwordStdin {
				if err := herald.SetHumanPassword(ctx, c, human.ID, pw); err != nil {
					return fmt.Errorf("owner %s created but set-password failed: %w", human.ID, err)
				}
				passwordSet = true
			}

			res := onboardResult{
				Org:             org.ID,
				OrgName:         org.Name,
				Owner:           human.ID,
				OwnerEmail:      human.DisplayName,
				Role:            roleOrgOwner,
				PasswordSet:     passwordSet,
				ConsoleURL:      console,
				CLIDownloadURL:  download,
				ProductsEnabled: productsEnabled,
			}
			return printResult(cmd.OutOrStdout(), gf.JSON, res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&owner, "owner", "", "owner's email / login (required)")
	f.StringArrayVar(&products, "product", nil, "product to enable (repeatable; default: all)")
	f.BoolVar(&passwordStdin, "owner-password-stdin", false, "read the owner's initial password from stdin")
	f.StringVar(&consoleURL, "console-url", "", "console base URL to print (default: the session edge)")
	f.StringVar(&cliDownloadURL, "cli-download-url", "", "CLI download URL to print (default: <console>/downloads)")
	return cmd
}

func newOffboardCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var confirm string
	cmd := &cobra.Command{
		Use:   "offboard <org-id> --confirm <org-name>",
		Short: "Purge a customer org (cascades to all its pillar data)",
		Long: "Offboard a customer org: herald deletes the org and fans the purge " +
			"out to every pillar (cairn/ledger/commonplace). --confirm must equal " +
			"the org name. This is irreversible.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(confirm) == "" {
				return fmt.Errorf("pass --confirm <org-name> to offboard (irreversible)")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			res, err := herald.DeleteOrg(cmd.Context(), c, args[0], confirm)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "offboarded %s (purged: %s)\n", res.Deleted, strings.Join(res.Pillars, ", "))
			return nil
		},
	}
	cmd.Flags().StringVar(&confirm, "confirm", "", "org name, required to confirm the purge")
	return cmd
}

func readPasswordStdin(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return "", fmt.Errorf("empty password on stdin")
	}
	return pw, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func printResult(w io.Writer, asJSON bool, res onboardResult) error {
	if asJSON {
		return json.NewEncoder(w).Encode(res)
	}
	if res.AlreadyOnboarded {
		fmt.Fprintf(w, "org %s (%s) already exists — nothing to do\n", res.Org, res.OrgName)
		fmt.Fprintf(w, "  products: %s\n", res.ProductsEnabled)
		fmt.Fprintf(w, "  console:  %s\n", res.ConsoleURL)
		fmt.Fprintf(w, "  cw CLI:   %s\n", res.CLIDownloadURL)
		return nil
	}
	fmt.Fprintf(w, "onboarded org %s (%s)\n", res.Org, res.OrgName)
	fmt.Fprintf(w, "  owner:    %s (%s), role %s\n", res.OwnerEmail, res.Owner, res.Role)
	if res.PasswordSet {
		fmt.Fprintf(w, "  password: set (owner can log in now)\n")
	} else {
		fmt.Fprintf(w, "  password: not set — run `cw human set-password %s`\n", res.Owner)
	}
	fmt.Fprintf(w, "  products: %s\n", res.ProductsEnabled)
	fmt.Fprintf(w, "  console:  %s\n", res.ConsoleURL)
	fmt.Fprintf(w, "  cw CLI:   %s\n", res.CLIDownloadURL)
	return nil
}
