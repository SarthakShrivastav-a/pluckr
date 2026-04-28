package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRemoveCmd(g *Globals) *cobra.Command {
	var keepFiles bool
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a source and its cache.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			reg, err := openRegistry(g)
			if err != nil {
				return err
			}
			if _, ok := reg.Get(name); !ok {
				return fmt.Errorf("no such source: %s", name)
			}
			if err := reg.Remove(name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s from registry\n", name)

			if !keepFiles {
				cache, err := openCache(g)
				if err != nil {
					return err
				}
				if err := cache.RemoveSource(name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "deleted cache directory for %s\n", name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&keepFiles, "keep-files", false, "Keep the on-disk markdown files; only remove the registry entry and FTS5 index.")
	return cmd
}
