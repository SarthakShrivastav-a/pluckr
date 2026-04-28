package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRootCmd(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "root",
		Short: "Print the resolved cache root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveRoot(g)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), r)
			return nil
		},
	}
}

// newMCPCmd returns the 'mcp' subcommand. The actual server lives in
// internal/mcp; cli/mcp.go is the registration shim.
