// Package org implements `cw org`: create, list, delete, products, enable, disable.
package org

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/spf13/cobra"
)

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "org", Short: "Manage orgs and product entitlements (herald admin)"}
	cmd.AddCommand(newCreateCmd(gf), newListCmd(gf), newDeleteCmd(gf),
		newProductsCmd(gf), newProductToggleCmd(gf, true), newProductToggleCmd(gf, false))
	return cmd
}

func newCreateCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var products []string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			o, err := herald.CreateOrg(cmd.Context(), c, herald.CreateOrgInput{Name: args[0], Products: products})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "created org %s (%s)\n", o.ID, o.Name)
			fmt.Println(o.ID)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&products, "product", nil, "product to enable at creation (repeatable)")
	return cmd
}

func newListCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List orgs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			orgs, err := herald.ListOrgs(cmd.Context(), c)
			if err != nil {
				return err
			}
			if gf.JSON {
				return json.NewEncoder(os.Stdout).Encode(orgs)
			}
			for _, o := range orgs {
				fmt.Printf("%-38s %s\n", o.ID, o.Name)
			}
			return nil
		},
	}
}

func newDeleteCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var confirm string
	cmd := &cobra.Command{
		Use:   "delete <id> --confirm <name>",
		Short: "Delete (purge) an org; --confirm must equal the org name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if confirm == "" {
				return fmt.Errorf("pass --confirm <org-name> to delete")
			}
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			res, err := herald.DeleteOrg(cmd.Context(), c, args[0], confirm)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "deleted %s (purged: %v)\n", res.Deleted, res.Pillars)
			return nil
		},
	}
	cmd.Flags().StringVar(&confirm, "confirm", "", "org name, required to confirm deletion")
	return cmd
}

func newProductsCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "products <org>",
		Short: "Show product entitlements for an org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			prods, err := herald.GetProducts(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			return printProducts(gf, prods)
		},
	}
}

// newProductToggleCmd builds both `enable` and `disable` (they differ only in
// the herald call + verb).
func newProductToggleCmd(gf *cmdutil.GlobalFlags, enable bool) *cobra.Command {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	return &cobra.Command{
		Use:   verb + " <org> <product>",
		Short: verb + " a product for an org",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := cmdutil.Session(gf)
			if err != nil {
				return err
			}
			fn := herald.DisableProduct
			if enable {
				fn = herald.EnableProduct
			}
			prods, err := fn(cmd.Context(), c, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "%sd %s for %s\n", verb, args[1], args[0])
			return printProducts(gf, prods)
		},
	}
}

// printProducts renders a {product:enabled} map: --json to stdout, else a
// sorted `product  enabled/disabled` table to stdout.
func printProducts(gf *cmdutil.GlobalFlags, prods map[string]bool) error {
	if gf.JSON {
		return json.NewEncoder(os.Stdout).Encode(prods)
	}
	keys := make([]string, 0, len(prods))
	for k := range prods {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		state := "disabled"
		if prods[k] {
			state = "enabled"
		}
		fmt.Printf("%-14s %s\n", k, state)
	}
	return nil
}
