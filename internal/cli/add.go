package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
)

// githubSpec recognises the GitHub spec form for kind detection.
var githubSpec = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+(@[A-Za-z0-9._/-]+)?(/[A-Za-z0-9._/-]+)?$`)

func newAddCmd(g *Globals) *cobra.Command {
	var (
		name     string
		kind     string
		refresh  string
		maxPages int
		pull     bool
	)
	cmd := &cobra.Command{
		Use:   "add <spec>",
		Short: "Subscribe to a docs source.",
		Long: `Add a source to the registry.

The spec accepts:
  https://react.dev/reference         (website or llms_txt)
  facebook/react                      (github)
  facebook/react@v18/docs             (github with ref + subdir)
  ./internal-docs                     (local folder)

The kind is detected from the spec; pass --kind to override.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec := args[0]
			detected := detectKind(spec)
			if kind == "" {
				kind = detected
			}
			if kind == "" {
				return fmt.Errorf("could not detect source kind for %q; pass --kind", spec)
			}
			if name == "" {
				name = deriveName(spec, kind)
			}

			entry := registry.Entry{
				Name:     name,
				Kind:     kind,
				Root:     spec,
				Refresh:  refresh,
				MaxPages: maxPages,
			}
			if err := entry.Validate(); err != nil {
				return err
			}
			reg, err := openRegistry(g)
			if err != nil {
				return err
			}
			if err := reg.Add(entry); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "added %s (%s) -> %s\n", entry.Name, entry.Kind, entry.Root)

			if pull {
				return runPull(cmd, g, []string{entry.Name})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "run 'pluckr pull %s' to fetch.\n", entry.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Source name (defaults to a sensible derivation of the spec).")
	cmd.Flags().StringVar(&kind, "kind", "", "Source kind: website | llms_txt | github | local. Detected when omitted.")
	cmd.Flags().StringVar(&refresh, "refresh", "7d", "Refresh interval: 7d | 30d | manual | never. Background refresh kicks in when an MCP session starts.")
	cmd.Flags().IntVar(&maxPages, "max", 0, "Maximum pages to pull (0 = source default).")
	cmd.Flags().BoolVar(&pull, "pull", false, "Run a pull immediately after adding.")
	return cmd
}

// DetectKind is the exported wrapper of detectKind so the MCP server's
// add_source tool can share the same logic without duplicating it.
func DetectKind(spec string) string { return detectKind(spec) }

// DeriveName is the exported wrapper of deriveName.
func DeriveName(spec, kind string) string { return deriveName(spec, kind) }

// detectKind picks a source kind from the spec, with hard-fail returning
// the empty string so the caller can prompt for --kind.
func detectKind(spec string) string {
	// Local: existing path or starts with ./, ../, /, or drive letter.
	if isLocalPath(spec) {
		return registry.KindLocal
	}
	if u, err := url.Parse(spec); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		// llms.txt convention: end of URL.
		if strings.HasSuffix(u.Path, "/llms.txt") || strings.HasSuffix(u.Path, "/llms-full.txt") {
			return registry.KindLLMSTxt
		}
		return registry.KindWebsite
	}
	if githubSpec.MatchString(spec) {
		return registry.KindGitHub
	}
	return ""
}

func isLocalPath(spec string) bool {
	if spec == "" {
		return false
	}
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "/") {
		return true
	}
	if len(spec) > 2 && spec[1] == ':' && (spec[2] == '/' || spec[2] == '\\') {
		// Windows drive letter
		return true
	}
	if strings.HasPrefix(spec, "~") {
		return true
	}
	if info, err := os.Stat(spec); err == nil && info.IsDir() {
		return true
	}
	return false
}

// deriveName picks a reasonable default name from the spec, depending
// on the kind.
func deriveName(spec, kind string) string {
	switch kind {
	case registry.KindLocal:
		abs, _ := filepath.Abs(spec)
		base := filepath.Base(abs)
		if base == "" || base == "." || base == "/" {
			return "local"
		}
		return base
	case registry.KindGitHub:
		// Keep owner/repo as the name; strip @ref and /subdir.
		s := spec
		if i := strings.Index(s, "@"); i >= 0 {
			rest := s[i+1:]
			if slash := strings.Index(rest, "/"); slash >= 0 {
				s = s[:i] + "/" + rest[slash+1:]
			} else {
				s = s[:i]
			}
		}
		// Trim subdir down to owner/repo
		parts := strings.SplitN(s, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return s
	default:
		// website / llms_txt: derive from hostname
		if u, err := url.Parse(spec); err == nil {
			if u.Hostname() != "" {
				return u.Hostname()
			}
		}
		return spec
	}
}
