package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SarthakShrivastav-a/pluckr/internal/chunk"
	"github.com/SarthakShrivastav-a/pluckr/internal/render"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever/fts5"
	"github.com/SarthakShrivastav-a/pluckr/internal/store"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func newReindexCmd(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "reindex <name>",
		Short: "Rebuild a source's FTS5 index from its on-disk markdown files.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cache, err := openCache(g)
			if err != nil {
				return err
			}
			pages, err := cache.ListPages(name)
			if err != nil {
				return err
			}
			if len(pages) == 0 {
				return fmt.Errorf("no pages on disk for %s; have you pulled?", name)
			}
			idx, err := fts5.Open(cache.IndexDBPath(name))
			if err != nil {
				return err
			}
			defer func() { _ = idx.Close() }()

			r := render.New()
			c := chunk.New()

			var allChunks []types.Chunk
			for _, p := range pages {
				body, err := cache.ReadPage(name, strings.TrimSuffix(p, ".md"))
				if err != nil {
					return err
				}
				stripped, fm := store.SplitFrontmatter(body)
				url := fm["url"]
				if url == "" {
					url = p
				}
				doc, err := r.Render(stripped, "text/markdown", url)
				if err != nil {
					return fmt.Errorf("render %s: %w", p, err)
				}
				doc.Source = name
				doc.Path = strings.TrimSuffix(p, ".md")
				if title := fm["title"]; title != "" {
					doc.Title = title
				}
				chunks := c.Chunk(doc)
				for i := range chunks {
					chunks[i].Source = name
					if chunks[i].Path == "" {
						chunks[i].Path = doc.Path
					}
					if chunks[i].Title == "" && doc.Title != "" {
						chunks[i].Title = doc.Title
					}
				}
				allChunks = append(allChunks, chunks...)
			}

			if err := idx.Reindex(context.Background(), name, allChunks); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "reindexed %s: %d pages, %d chunks\n", name, len(pages), len(allChunks))
			return nil
		},
	}
}

