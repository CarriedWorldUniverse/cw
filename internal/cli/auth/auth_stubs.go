package auth

// TEMPORARY STUBS — Task 8 replaces these with real implementations.
// newLogoutCmd/newSwitchCmd belong to Task 8. Delete this file when it lands.

import (
	"errors"

	"github.com/spf13/cobra"
)

func newLogoutCmd(_ *GlobalFlags) *cobra.Command {
	return &cobra.Command{Use: "logout", Short: "Log out of a context (not implemented)", RunE: notImplemented}
}

func newSwitchCmd(_ *GlobalFlags) *cobra.Command {
	return &cobra.Command{Use: "switch", Short: "Switch the current context (not implemented)", RunE: notImplemented}
}

func notImplemented(_ *cobra.Command, _ []string) error { return errors.New("not implemented") }
