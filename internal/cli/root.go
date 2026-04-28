// Package cli implements the pluckr command-line interface using cobra.
//
// Commands stay narrow: each one is a thin layer that wires user input
// into the registry, pipeline, retriever, or MCP server. Heavy lifting
// belongs in those packages, not here.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever/fts5"
	"github.com/SarthakShrivastav-a/pluckr/internal/store"
)

// Globals carries the values flags collect at the root level so each
// subcommand can grab them via a context-friendly accessor without
// re-parsing.
type Globals struct {
	CacheRoot string
}

// NewRoot returns the configured root cobra command.
func NewRoot(version string) *cobra.Command {
	g := &Globals{}

	root := &cobra.Command{
		Use:           "pluckr",
		Short:         "Local-first, agent-native docs cache.",
		Long:          "pluckr ingests docs sites, llms.txt endpoints, GitHub repos, and local folders into a markdown cache and serves it to LLM agents over MCP.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&g.CacheRoot, "cache", "", "Cache root directory (defaults to ~/.pluckr).")

	root.AddCommand(
		newAddCmd(g),
		newListCmd(g),
		newRemoveCmd(g),
		newPullCmd(g),
		newSearchCmd(g),
		newMCPCmd(g),
		newReindexCmd(g),
		newRootCmd(g),
	)
	return root
}

// resolveRoot returns the cache root, honoring --cache when set.
func resolveRoot(g *Globals) (string, error) {
	root, err := store.Root(g.CacheRoot)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("cli: create cache root: %w", err)
	}
	return root, nil
}

// openCache opens the on-disk cache rooted at the resolved root.
func openCache(g *Globals) (*store.Cache, error) {
	root, err := resolveRoot(g)
	if err != nil {
		return nil, err
	}
	return store.Open(root)
}

// openRegistry opens the registry from the resolved cache root.
func openRegistry(g *Globals) (*registry.Registry, error) {
	root, err := resolveRoot(g)
	if err != nil {
		return nil, err
	}
	return registry.Load(filepath.Join(root, "registry.json"))
}

// openIndex opens (or creates) the FTS5 index for the named source.
func openIndex(cache *store.Cache, sourceName string) (retriever.Retriever, error) {
	if err := cache.EnsureSource(sourceName); err != nil {
		return nil, err
	}
	return fts5.Open(cache.IndexDBPath(sourceName))
}

// errOut writes an error message in pluckr's standard prefix style.
func errOut(out io.Writer, format string, a ...any) {
	fmt.Fprintf(out, "pluckr: "+format+"\n", a...)
}
