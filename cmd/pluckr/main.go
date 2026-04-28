// Command pluckr is the CLI entry point. The bulk of the work lives in
// internal/cli; main.go just hands off to NewRoot().Execute().
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/SarthakShrivastav-a/pluckr/internal/cli"
	// Linked for its init() side effect: registers the MCP server runner
	// with the CLI so 'pluckr mcp' works.
	_ "github.com/SarthakShrivastav-a/pluckr/internal/mcp"
)

// Version is overwritten at build time via -ldflags="-X main.Version=...".
// When unset (e.g. plain 'go install' without ldflags) we fall back to
// runtime/debug.ReadBuildInfo so users still see something meaningful.
var Version = ""

func main() {
	cmd := cli.NewRoot(resolveVersion())
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "pluckr:", err)
		os.Exit(1)
	}
}

func resolveVersion() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	v := info.Main.Version
	switch {
	case v == "" || v == "(devel)":
		return "dev"
	case strings.HasPrefix(v, "v0.0.0-"):
		// pseudo-version: keep the short SHA part for context
		parts := strings.Split(v, "-")
		if len(parts) >= 3 {
			return "dev+" + parts[len(parts)-1]
		}
		return v
	default:
		return v
	}
}
