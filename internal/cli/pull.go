package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/pipeline"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
)

// renderFlag is read by both 'pluckr pull' and 'pluckr add --pull' so a
// single declaration here keeps the two paths consistent.
var renderFlag bool

func newPullCmd(g *Globals) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "pull [name...]",
		Short: "Fetch and index one, several, or all sources.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				return runPull(cmd, g, nil)
			}
			if len(args) == 0 {
				return fmt.Errorf("specify a source name or use --all")
			}
			return runPull(cmd, g, args)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Pull every subscribed source.")
	cmd.Flags().BoolVar(&renderFlag, "render", false, "Escalate empty-content pages through a headless browser. Requires Chrome / Chromium installed on PATH.")
	return cmd
}

// runPull is shared with 'pluckr add --pull' so the call site stays
// consistent.
func runPull(cmd *cobra.Command, g *Globals, names []string) error {
	reg, err := openRegistry(g)
	if err != nil {
		return err
	}
	cache, err := openCache(g)
	if err != nil {
		return err
	}

	entries := reg.List()
	if len(names) > 0 {
		filtered := make([]registry.Entry, 0, len(names))
		for _, n := range names {
			e, ok := reg.Get(n)
			if !ok {
				return fmt.Errorf("no such source: %s", n)
			}
			filtered = append(filtered, e)
		}
		entries = filtered
	}
	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no sources to pull.")
		return nil
	}

	for _, entry := range entries {
		idx, err := openIndex(cache, entry.Name)
		if err != nil {
			return err
		}

		p := pipeline.New(cache, idx)
		p.Lookup = os.Getenv

		var headless *fetch.HeadlessFetcher
		if renderFlag {
			headless = fetch.NewHeadless()
			p.Escalator = headless
		}

		fmt.Fprintf(cmd.OutOrStdout(), "pulling %s (%s)...\n", entry.Name, entry.Kind)
		ctx, cancel := context.WithCancel(cmd.Context())
		res, err := p.Run(ctx, entry)
		cancel()
		_ = idx.Close()

		if headless != nil {
			_ = headless.Close()
		}

		if err != nil {
			return fmt.Errorf("pull %s: %w", entry.Name, err)
		}

		if uerr := reg.Update(entry.Name, func(e registry.Entry) registry.Entry {
			e.LastSyncedAt = time.Now().UTC()
			return e
		}); uerr != nil {
			return fmt.Errorf("update registry: %w", uerr)
		}

		fmt.Fprintf(cmd.OutOrStdout(),
			"  %s: %d pages, %d chunks, %d skipped, %d errors in %s\n",
			entry.Name, res.Pages, res.Chunks, res.Skipped, len(res.Errors),
			res.Elapsed.Truncate(100*time.Millisecond),
		)
		if len(res.Errors) > 0 {
			for _, e := range res.Errors[:min(3, len(res.Errors))] {
				fmt.Fprintf(cmd.OutOrStdout(), "    ! %v\n", e)
			}
			if len(res.Errors) > 3 {
				fmt.Fprintf(cmd.OutOrStdout(), "    ... and %d more\n", len(res.Errors)-3)
			}
		}
	}
	return nil
}
