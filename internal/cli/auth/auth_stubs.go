package auth

// TEMPORARY STUBS — Tasks 7-8 replace these with real implementations.
// newWhoamiCmd/newTokenCmd/newStatusCmd belong to Task 7; newLogoutCmd/
// newSwitchCmd to Task 8. Delete this file when those tasks land.

import (
	"errors"

	"github.com/spf13/cobra"
)

func newLogoutCmd(_ *GlobalFlags) *cobra.Command {
	return &cobra.Command{Use: "logout", Short: "Log out of a context (not implemented)", RunE: notImplemented}
}

func newWhoamiCmd(_ *GlobalFlags) *cobra.Command {
	return &cobra.Command{Use: "whoami", Short: "Show the current identity (not implemented)", RunE: notImplemented}
}

func newStatusCmd(_ *GlobalFlags) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show auth status (not implemented)", RunE: notImplemented}
}

func newSwitchCmd(_ *GlobalFlags) *cobra.Command {
	return &cobra.Command{Use: "switch", Short: "Switch the current context (not implemented)", RunE: notImplemented}
}

func newTokenCmd(_ *GlobalFlags) *cobra.Command {
	return &cobra.Command{Use: "token", Short: "Print a valid access token (not implemented)", RunE: notImplemented}
}

func notImplemented(_ *cobra.Command, _ []string) error { return errors.New("not implemented") }
