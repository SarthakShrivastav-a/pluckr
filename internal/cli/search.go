package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever/fts5"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func newSearchCmd(g *Globals) *cobra.Command {
	var (
		sources []string
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the cache via FTS5/BM25.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			reg, err := openRegistry(g)
			if err != nil {
				return err
			}
			cache, err := openCache(g)
			if err != nil {
				return err
			}

			targets := reg.List()
			if len(sources) > 0 {
				filtered := make([]registry.Entry, 0, len(sources))
				for _, s := range sources {
					if e, ok := reg.Get(s); ok {
						filtered = append(filtered, e)
					}
				}
				targets = filtered
			}
			if len(targets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sources to search. add some with 'pluckr add'.")
				return nil
			}

			var allHits []types.Hit
			for _, t := range targets {
				dbPath := cache.IndexDBPath(t.Name)
				idx, err := fts5.Open(dbPath)
				if err != nil {
					continue
				}
				hits, err := idx.Search(context.Background(), query, retriever.SearchOptions{Limit: limit})
				_ = idx.Close()
				if err != nil {
					continue
				}
				for i := range hits {
					if hits[i].Source == "" {
						hits[i].Source = t.Name
					}
					hits[i].LastSyncedAt = t.LastSyncedAt
				}
				allHits = append(allHits, hits...)
			}
			if len(allHits) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no hits for %q.\n", query)
				return nil
			}
			for i, h := range allHits {
				heading := strings.Join(h.HeadingPath, " › ")
				if heading == "" {
					heading = "(intro)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d. %s › %s\n", i+1, h.Source, heading)
				fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", h.URL)
				fmt.Fprintf(cmd.OutOrStdout(), "   %s\n\n", trimSnippet(h.Snippet))
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&sources, "source", nil, "Restrict the search to one or more source names. Repeatable.")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum hits per source.")
	return cmd
}

func trimSnippet(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 240 {
		return s[:237] + "..."
	}
	return s
}
