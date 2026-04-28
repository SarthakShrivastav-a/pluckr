// Package mcp implements the pluckr MCP server. It exposes the docs
// cache to LLM agents via the Model Context Protocol over stdio,
// using github.com/mark3labs/mcp-go.
//
// The seven v0.1 tools are split into two tiers:
//
//   - Read tools (always on): search_docs, get_page, list_sources, get_outline.
//   - Mutating tools: refresh_source, add_source, remove_source. These
//     are enabled by default - the MCP host's per-call consent UI is
//     the user-facing gate - but can be turned off entirely by setting
//     PLUCKR_MCP_NO_MUTATIONS=true for users who want the binary to
//     refuse them server-side.
//
// Run() registers itself with the CLI via cli.SetMCPRunner so the
// 'pluckr mcp' command works without internal/cli pulling in MCP-go.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/SarthakShrivastav-a/pluckr/internal/chunk"
	"github.com/SarthakShrivastav-a/pluckr/internal/cli"
	"github.com/SarthakShrivastav-a/pluckr/internal/pipeline"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/render"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever/fts5"
	"github.com/SarthakShrivastav-a/pluckr/internal/store"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// stalenessAfter is the threshold beyond which a hit is flagged stale.
const stalenessAfter = 7 * 24 * time.Hour

func init() {
	cli.SetMCPRunner(Run)
}

// Run is the entry point invoked by the CLI's `pluckr mcp` command.
func Run(ctx context.Context, g *cli.Globals) error {
	root, err := store.Root(g.CacheRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("mcp: ensure cache root: %w", err)
	}
	cache, err := store.Open(root)
	if err != nil {
		return err
	}
	reg, err := registry.Load(filepath.Join(root, "registry.json"))
	if err != nil {
		return err
	}

	srv := buildServer(cache, reg, mutationsAllowed())
	return mcpserver.ServeStdio(srv)
}

// mutationsAllowed reads PLUCKR_MCP_NO_MUTATIONS; the env var is the
// user's last-resort kill switch for server-side mutation tools.
func mutationsAllowed() bool {
	v := strings.ToLower(os.Getenv("PLUCKR_MCP_NO_MUTATIONS"))
	switch v {
	case "1", "true", "yes":
		return false
	}
	return true
}

func buildServer(cache *store.Cache, reg *registry.Registry, allowMutations bool) *mcpserver.MCPServer {
	srv := mcpserver.NewMCPServer(
		"pluckr",
		"0.1",
		mcpserver.WithToolCapabilities(true),
	)

	registerSearchDocs(srv, cache, reg)
	registerGetPage(srv, cache, reg)
	registerListSources(srv, cache, reg)
	registerGetOutline(srv, cache, reg)

	if allowMutations {
		registerRefreshSource(srv, cache, reg)
		registerAddSource(srv, cache, reg)
		registerRemoveSource(srv, cache, reg)
	}
	return srv
}

// ---------- search_docs ----------

func registerSearchDocs(srv *mcpserver.MCPServer, cache *store.Cache, reg *registry.Registry) {
	tool := mcpgo.NewTool("search_docs",
		mcpgo.WithDescription("Search the cached docs by full-text query (BM25). Returns chunks with their source, URL, heading path, a snippet, and freshness metadata."),
		mcpgo.WithString("query", mcpgo.Description("Search terms. Natural language is fine; the tokenizer ignores punctuation."), mcpgo.Required()),
		mcpgo.WithArray("sources", mcpgo.Description("Optional list of source names to restrict the search to. Empty means all sources.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum number of hits per source. Defaults to 10.")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		sources := req.GetStringSlice("sources", nil)
		limit := req.GetInt("limit", 10)

		entries := selectEntries(reg, sources)
		var allHits []types.Hit
		for _, e := range entries {
			idx, err := fts5.Open(cache.IndexDBPath(e.Name))
			if err != nil {
				continue
			}
			hits, err := idx.Search(ctx, query, retriever.SearchOptions{Limit: limit})
			_ = idx.Close()
			if err != nil {
				continue
			}
			for i := range hits {
				if hits[i].Source == "" {
					hits[i].Source = e.Name
				}
				hits[i].LastSyncedAt = e.LastSyncedAt
				if !e.LastSyncedAt.IsZero() && time.Since(e.LastSyncedAt) > stalenessAfter {
					hits[i].Stale = true
				}
			}
			allHits = append(allHits, hits...)
		}
		return jsonResult(allHits)
	})
}

// ---------- get_page ----------

func registerGetPage(srv *mcpserver.MCPServer, cache *store.Cache, reg *registry.Registry) {
	tool := mcpgo.NewTool("get_page",
		mcpgo.WithDescription("Return the full markdown of a single cached page. Use this when search_docs returns a chunk and the agent wants the surrounding context."),
		mcpgo.WithString("source", mcpgo.Description("Source name (as listed by list_sources)."), mcpgo.Required()),
		mcpgo.WithString("path", mcpgo.Description("Source-relative page path, with or without the .md extension."), mcpgo.Required()),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		source, err := req.RequireString("source")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		path, err := req.RequireString("path")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		body, err := cache.ReadPage(source, strings.TrimSuffix(path, ".md"))
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("get_page: %v", err)), nil
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

// ---------- list_sources ----------

func registerListSources(srv *mcpserver.MCPServer, cache *store.Cache, reg *registry.Registry) {
	tool := mcpgo.NewTool("list_sources",
		mcpgo.WithDescription("Return the list of subscribed sources with their kind, root, page count, last sync, and a stale flag."),
	)
	srv.AddTool(tool, func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		entries := reg.List()
		out := make([]types.SourceInfo, 0, len(entries))
		for _, e := range entries {
			pages, _ := cache.ListPages(e.Name)
			info := types.SourceInfo{
				Name:         e.Name,
				Kind:         e.Kind,
				Root:         e.Root,
				PageCount:    len(pages),
				LastSyncedAt: e.LastSyncedAt,
				RefreshAfter: e.Refresh,
			}
			if !e.LastSyncedAt.IsZero() && time.Since(e.LastSyncedAt) > stalenessAfter {
				info.Stale = true
			}
			out = append(out, info)
		}
		return jsonResult(out)
	})
}

// ---------- get_outline ----------

type outlineNode struct {
	Source      string   `json:"source"`
	Path        string   `json:"path"`
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	HeadingPath []string `json:"heading_path"`
	Anchor      string   `json:"anchor,omitempty"`
}

func registerGetOutline(srv *mcpserver.MCPServer, cache *store.Cache, reg *registry.Registry) {
	tool := mcpgo.NewTool("get_outline",
		mcpgo.WithDescription("Return the heading tree of every page in a source. Useful for an agent that wants to navigate the docs without searching first."),
		mcpgo.WithString("source", mcpgo.Description("Source name."), mcpgo.Required()),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		source, err := req.RequireString("source")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		pages, err := cache.ListPages(source)
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("get_outline: %v", err)), nil
		}
		r := render.New()
		c := chunk.New()
		var nodes []outlineNode
		for _, p := range pages {
			body, err := cache.ReadPage(source, strings.TrimSuffix(p, ".md"))
			if err != nil {
				continue
			}
			stripped, fm := splitFrontmatter(body)
			doc, err := r.Render(stripped, "text/markdown", urlFromFrontmatter(fm, p))
			if err != nil {
				continue
			}
			doc.Source = source
			doc.Path = strings.TrimSuffix(p, ".md")
			chunks := c.Chunk(doc)
			for _, ch := range chunks {
				nodes = append(nodes, outlineNode{
					Source:      source,
					Path:        doc.Path,
					URL:         doc.URL,
					Title:       doc.Title,
					HeadingPath: ch.HeadingPath,
					Anchor:      ch.Anchor,
				})
			}
		}
		return jsonResult(nodes)
	})
}

// ---------- refresh_source ----------

func registerRefreshSource(srv *mcpserver.MCPServer, cache *store.Cache, reg *registry.Registry) {
	tool := mcpgo.NewTool("refresh_source",
		mcpgo.WithDescription("Re-run the pipeline for one source so the cache catches up to the latest content. Returns a summary."),
		mcpgo.WithString("name", mcpgo.Description("Source name."), mcpgo.Required()),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		entry, ok := reg.Get(name)
		if !ok {
			return mcpgo.NewToolResultError(fmt.Sprintf("no such source: %s", name)), nil
		}
		idx, err := fts5.Open(cache.IndexDBPath(entry.Name))
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		defer func() { _ = idx.Close() }()
		p := pipeline.New(cache, idx)
		p.Lookup = os.Getenv
		res, err := p.Run(ctx, entry)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		_ = reg.Update(entry.Name, func(e registry.Entry) registry.Entry {
			e.LastSyncedAt = time.Now().UTC()
			return e
		})
		return jsonResult(res)
	})
}

// ---------- add_source ----------

func registerAddSource(srv *mcpserver.MCPServer, cache *store.Cache, reg *registry.Registry) {
	tool := mcpgo.NewTool("add_source",
		mcpgo.WithDescription("Subscribe to a new source. Caller may pass kind explicitly or omit it for spec-based detection."),
		mcpgo.WithString("spec", mcpgo.Description("Spec: URL for website / llms_txt, owner/repo[@ref][/subdir] for github, or filesystem path for local."), mcpgo.Required()),
		mcpgo.WithString("name", mcpgo.Description("Friendly name. Defaults to a derivation of the spec.")),
		mcpgo.WithString("kind", mcpgo.Description("One of website, llms_txt, github, local. Detected from the spec when omitted.")),
		mcpgo.WithString("refresh", mcpgo.Description("Refresh interval such as 7d, 30d, manual, never. Defaults to 7d.")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		spec, err := req.RequireString("spec")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		kind := req.GetString("kind", "")
		name := req.GetString("name", "")
		refresh := req.GetString("refresh", "7d")
		if kind == "" {
			kind = cli.DetectKind(spec)
		}
		if kind == "" {
			return mcpgo.NewToolResultError("could not detect source kind; pass kind explicitly"), nil
		}
		if name == "" {
			name = cli.DeriveName(spec, kind)
		}
		entry := registry.Entry{Name: name, Kind: kind, Root: spec, Refresh: refresh}
		if err := entry.Validate(); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := reg.Add(entry); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return jsonResult(types.SourceInfo{
			Name: entry.Name, Kind: entry.Kind, Root: entry.Root, RefreshAfter: entry.Refresh,
		})
	})
}

// ---------- remove_source ----------

func registerRemoveSource(srv *mcpserver.MCPServer, cache *store.Cache, reg *registry.Registry) {
	tool := mcpgo.NewTool("remove_source",
		mcpgo.WithDescription("Remove a source from the registry and (by default) delete its cached files."),
		mcpgo.WithString("name", mcpgo.Description("Source name."), mcpgo.Required()),
		mcpgo.WithBoolean("keep_files", mcpgo.Description("If true, the on-disk markdown files are kept; only the registry entry and FTS5 index are removed.")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		keepFiles := req.GetBool("keep_files", false)
		if err := reg.Remove(name); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if !keepFiles {
			if err := cache.RemoveSource(name); err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
		}
		return mcpgo.NewToolResultText("ok"), nil
	})
}

// ---------- helpers ----------

func selectEntries(reg *registry.Registry, names []string) []registry.Entry {
	if len(names) == 0 {
		return reg.List()
	}
	var out []registry.Entry
	for _, n := range names {
		if e, ok := reg.Get(n); ok {
			out = append(out, e)
		}
	}
	return out
}

func jsonResult(v any) (*mcpgo.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	return mcpgo.NewToolResultText(string(data)), nil
}

// splitFrontmatter / urlFromFrontmatter mirror the cli helpers; kept
// inlined here to avoid an awkward import dependency from the cli
// package on its own subpackages.
func splitFrontmatter(body []byte) ([]byte, string) {
	s := string(body)
	if !strings.HasPrefix(s, "---\n") {
		return body, ""
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return body, ""
	}
	frontmatter := rest[:end]
	markdown := strings.TrimLeft(rest[end+5:], "\n")
	return []byte(markdown), frontmatter
}

func urlFromFrontmatter(fm, fallback string) string {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "url:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "url:"))
			v = strings.Trim(v, `"`)
			if v != "" {
				return v
			}
		}
	}
	return fallback
}
