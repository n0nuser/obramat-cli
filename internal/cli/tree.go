package cli

import "github.com/spf13/cobra"

// CommandTree returns the wired command tree without running it, for tools
// that document the binary from its own definitions (cmd/docsgen).
func CommandTree(version string) *cobra.Command {
	return (&App{Version: version}).rootCmd()
}
