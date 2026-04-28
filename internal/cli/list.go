package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newListCmd(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List subscribed sources.",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := openRegistry(g)
			if err != nil {
				return err
			}
			entries := reg.List()
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sources subscribed yet. try 'pluckr add <spec>'.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tKIND\tROOT\tREFRESH\tLAST SYNCED")
			for _, e := range entries {
				last := "—"
				if !e.LastSyncedAt.IsZero() {
					last = humanAgo(e.LastSyncedAt)
				}
				refresh := e.Refresh
				if refresh == "" {
					refresh = "7d"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Kind, e.Root, refresh, last)
			}
			return w.Flush()
		},
	}
}

// humanAgo formats t as a coarse-grained duration since now: "5m ago",
// "3h ago", "2d ago", or an ISO date for older times.
func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	default:
		return t.UTC().Format("2006-01-02")
	}
}
