// Package retriever defines the search interface and the value types
// callers use to drive it. The default FTS5 implementation lives in the
// fts5 subpackage; future vector or hybrid backends slot in by satisfying
// the same interface.
package retriever

import (
	"context"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// SearchOptions tunes a single search call. Zero values are safe.
type SearchOptions struct {
	// Sources optionally restricts results to a subset of source names.
	// Empty means "all sources".
	Sources []string
	// Limit caps the number of returned hits. Zero falls back to 10.
	Limit int
	// Offset skips this many hits before returning. Zero means none.
	Offset int
}

// Retriever is the operational interface every backend satisfies.
type Retriever interface {
	// Index inserts chunks for sourceName, leaving any previous chunks
	// for that source in place. Use Reindex to replace.
	Index(ctx context.Context, sourceName string, chunks []types.Chunk) error
	// Reindex atomically wipes and replaces a source's chunks.
	Reindex(ctx context.Context, sourceName string, chunks []types.Chunk) error
	// Search returns ranked hits for the given query.
	Search(ctx context.Context, query string, opts SearchOptions) ([]types.Hit, error)
	// Remove deletes all chunks for sourceName.
	Remove(ctx context.Context, sourceName string) error
	// Sources lists the distinct source names currently indexed.
	Sources(ctx context.Context) ([]string, error)
	// Close releases any underlying resources.
	Close() error
}
