// Command pluckr is the CLI entry point. The bulk of the work lives in
// internal/cli; main.go just hands off to NewRoot().Execute().
package main

import (
	"fmt"
	"os"

	"github.com/SarthakShrivastav-a/pluckr/internal/cli"
	// Linked for its init() side effect: registers the MCP server runner
	// with the CLI so 'pluckr mcp' works.
	_ "github.com/SarthakShrivastav-a/pluckr/internal/mcp"
)

// Version is overwritten at build time via -ldflags="-X main.Version=...".
var Version = "dev"

func main() {
	cmd := cli.NewRoot(Version)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "pluckr:", err)
		os.Exit(1)
	}
}
