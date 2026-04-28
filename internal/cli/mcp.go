package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

// MCPRunner is the function internal/mcp registers so the CLI can
// invoke it without importing the MCP package directly (which would
// create a cycle once the MCP server starts pulling in pipeline /
// retriever / store helpers that already live downstream of cli).
type MCPRunner func(ctx context.Context, g *Globals) error

var mcpRunner MCPRunner

// SetMCPRunner registers the runner. internal/mcp.Register() calls this
// from main.go.
func SetMCPRunner(fn MCPRunner) { mcpRunner = fn }

var errMCPNotLinked = errors.New("mcp: server package not linked; rebuild with the mcp tag enabled")

func newMCPCmd(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the cache to LLM agents over MCP (stdio).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if mcpRunner == nil {
				return errMCPNotLinked
			}
			return mcpRunner(cmd.Context(), g)
		},
	}
}
