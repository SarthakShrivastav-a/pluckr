# pluckr

Local-first, agent-native docs cache. Pull docs sites, `llms.txt` endpoints, GitHub repos, and local folders into a markdown cache that any LLM agent can search via MCP.

```text
$ pluckr add https://react.dev/reference --pull
  added react.dev (website) -> https://react.dev/reference
  pulling react.dev (website)...
    react.dev: 412 pages, 3,180 chunks, 6 skipped, 0 errors in 18.2s

$ pluckr search "useEffect cleanup"
1. react.dev › Reference › useEffect › Cleaning up an effect
   https://react.dev/reference/react/useEffect#cleaning-up-an-effect
   ... return a [cleanup] function from the Effect ...

$ pluckr mcp     # serve the cache to Claude Code, Cursor, anywhere MCP runs
```

## Why

LLM agents currently choose between three bad options for docs:

- dump entire docs sites into context (wasteful, expensive, fragile),
- browse live (slow, flaky, blocked by SPAs and rate limits),
- hope their training data is fresh (it isn't).

pluckr is a fourth option. One static binary keeps a folder of clean markdown plus a tiny SQLite full-text index. An MCP server exposes that cache to any compatible host (Claude Code, Cursor, Claude Desktop, Cline, Zed, ...). The agent searches once, gets exactly the section it needs, and cites the original URL.

## Install

```bash
# macOS / Linux
brew install SarthakShrivastav-a/tap/pluckr

# Windows
scoop bucket add pluckr https://github.com/SarthakShrivastav-a/scoop-bucket
scoop install pluckr

# Anywhere with Go 1.25+
go install github.com/SarthakShrivastav-a/pluckr/cmd/pluckr@latest
```

## Quick start

```bash
# subscribe to a few sources
pluckr add https://react.dev/reference
pluckr add https://docs.python.org/3 --kind website --max 200
pluckr add facebook/react/docs              # github
pluckr add https://example.com/llms.txt     # llms.txt convention
pluckr add ~/notes-vault                    # local markdown folder

# fetch + index everything
pluckr pull --all

# search from the CLI
pluckr search "useState"

# serve to your agent
pluckr mcp
```

## Source kinds

| Kind | Spec example | What it does |
| --- | --- | --- |
| `website` | `https://react.dev/reference` | Sitemap → nav → BFS crawl, fetch each URL, render to markdown. |
| `llms_txt` | `https://example.com/llms.txt` | Prefer `/llms-full.txt` if present; otherwise parse links from `/llms.txt`. |
| `github` | `facebook/react/docs` | List the repo tree, pull every `.md` / `.markdown` / `.mdx` under the optional subdir. |
| `local` | `~/internal-docs` | Walk a folder, pick up `.md` / `.markdown` / `.mdx` / `.txt`. No network needed. |

The kind is detected from the spec; pass `--kind` to override.

## Hooking it up

### Claude Code

```bash
claude mcp add pluckr -- pluckr mcp
```

Or install the bundled plugin (MCP server + skill + slash commands + SessionStart hook):

```bash
claude plugin install github.com/SarthakShrivastav-a/pluckr/plugin
```

### Cursor

Add to `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "pluckr": { "command": "pluckr", "args": ["mcp"] }
  }
}
```

### Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "pluckr": { "command": "pluckr", "args": ["mcp"] }
  }
}
```

## MCP tools exposed

| Tool | Purpose |
| --- | --- |
| `search_docs(query, sources?, limit?)` | BM25 search across all subscribed sources. Returns chunks with heading_path, snippet, freshness. |
| `get_page(source, path)` | Full markdown of one cached page. |
| `list_sources()` | Subscribed sources with kind, root, page count, last sync, stale flag. |
| `get_outline(source)` | Heading tree of an entire source. |
| `refresh_source(name)` | Re-run the pipeline for one source. *Mutating; gated.* |
| `add_source(spec, name?, kind?, refresh?)` | Subscribe a new source. *Mutating; gated.* |
| `remove_source(name, keep_files?)` | Drop a source from the registry. *Mutating; gated.* |

The MCP host's per-call consent UI is the user-facing safety gate for mutating tools. Set `PLUCKR_MCP_NO_MUTATIONS=true` to disable them server-side.

## CLI reference

```text
pluckr add <spec> [--name --kind --refresh --max --pull]
pluckr list
pluckr remove <name> [--keep-files]
pluckr pull [name...] [--all]
pluckr search <query> [--source --limit]
pluckr reindex <name>
pluckr mcp
pluckr root
```

## On-disk layout

```text
~/.pluckr/
  registry.json
  sources/
    react.dev/
      pages/                    # markdown is the source of truth
        reference/hooks/useState.md
      manifest.json             # per-page hash, fetched_at, token_count
      index.db                  # SQLite FTS5; rebuildable from pages/
```

Markdown files are the source of truth - hand-edit them, run `pluckr reindex <source>`, and the FTS5 index catches up.

## Configuration

Headers and cookies expand `${ENV}` references at fetch time, so secrets stay out of the registry file:

```json
{
  "name": "internal-confluence",
  "kind": "website",
  "root": "https://wiki.corp.example/spaces/DOCS",
  "headers": { "Cookie": "JSESSIONID=${WIKI_SESSION}" }
}
```

The `refresh` field accepts `7d`, `30d`, `manual`, or `never`. The MCP server kicks off background refresh of overdue sources at session start.

## Status

This is v0.1. The four source kinds, FTS5 search, the MCP read tools, and the CLI are all working. Headless rendering for SPA-only docs sites and pluggable vector retrievers are designed-in but not yet built. See [docs/design.md](docs/design.md) for the full spec.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Bug reports, source-kind PRs, and renderer improvements are all welcome.

## License

[MIT](LICENSE).
