<div align="center">

# pluckr

**Local-first, agent-native docs cache.**

Pull docs sites, GitHub repos, `llms.txt` endpoints, or local folders into a markdown cache. Search via SQLite FTS5. Serve to Claude Code, Cursor, and any MCP host.

[![ci](https://github.com/SarthakShrivastav-a/pluckr/actions/workflows/ci.yml/badge.svg)](https://github.com/SarthakShrivastav-a/pluckr/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/SarthakShrivastav-a/pluckr?include_prereleases&sort=semver)](https://github.com/SarthakShrivastav-a/pluckr/releases)
[![go reference](https://pkg.go.dev/badge/github.com/SarthakShrivastav-a/pluckr.svg)](https://pkg.go.dev/github.com/SarthakShrivastav-a/pluckr)
[![go report](https://goreportcard.com/badge/github.com/SarthakShrivastav-a/pluckr)](https://goreportcard.com/report/github.com/SarthakShrivastav-a/pluckr)
[![license](https://img.shields.io/github/license/SarthakShrivastav-a/pluckr)](LICENSE)

</div>

---

## Why

Today, LLM agents only have three options for docs:

- **dump entire sites into context** — wasteful, expensive, fragile
- **browse live** — slow, flaky, blocked by SPAs and rate limits
- **hope training data is fresh** — it isn't

`pluckr` is option four. One static binary keeps a folder of clean markdown plus a tiny SQLite full-text index. An MCP server exposes that cache to any compatible host (Claude Code, Cursor, Claude Desktop, Cline, Zed, …). Your agent searches once, gets exactly the section it needs, and cites the original URL.

## See it in action

```text
$ pluckr add https://react.dev/reference --pull
  added react.dev (website) → https://react.dev/reference
  pulling react.dev (website)...
    react.dev: 412 pages, 3,180 chunks, 6 skipped, 0 errors in 18.2s

$ pluckr search "useEffect cleanup"
1. react.dev › Reference › useEffect › Cleaning up an effect
   https://react.dev/reference/react/useEffect#cleaning-up-an-effect
   ... return a [cleanup] function from the Effect ...

$ pluckr mcp     # serve the cache over MCP/stdio
```

## Install

`pluckr` ships as a single static Go binary. Three install paths today:

```bash
# 1. Latest release (recommended)
go install github.com/SarthakShrivastav-a/pluckr/cmd/pluckr@latest

# 2. Specific version
go install github.com/SarthakShrivastav-a/pluckr/cmd/pluckr@v0.2.2

# 3. From source
git clone https://github.com/SarthakShrivastav-a/pluckr.git
cd pluckr && go build -o pluckr ./cmd/pluckr
```

Requires Go 1.25+. Pre-built binaries for Linux / macOS / Windows on amd64 and arm64 are attached to every [GitHub release](https://github.com/SarthakShrivastav-a/pluckr/releases) — download, unzip, drop on `PATH`.

## Quick start

```bash
# subscribe to a few sources (kind is detected from the spec)
pluckr add https://react.dev/reference
pluckr add https://docs.python.org/3 --max 200
pluckr add facebook/react/docs              # github
pluckr add https://example.com/llms.txt     # llms.txt convention
pluckr add ~/internal-docs                  # local markdown folder

# fetch + index everything
pluckr pull --all

# search from the CLI
pluckr search "useState"

# serve to your agent
pluckr mcp
```

## Source kinds

| Kind | Spec | What it does |
|---|---|---|
| `website` | `https://react.dev/reference` | Sitemap → nav → BFS crawl, fetch each URL, render to clean markdown |
| `llms_txt` | `https://example.com/llms.txt` | Prefer `/llms-full.txt` if present; otherwise parse links from `/llms.txt` |
| `github` | `facebook/react/docs` | List the repo tree, pull every `.md` / `.markdown` / `.mdx` under the optional subdir |
| `local` | `~/internal-docs` | Walk a folder, pick up `.md` / `.markdown` / `.mdx` / `.txt`. No network |

The kind is detected from the spec; pass `--kind` to override.

## Hooking it up to an agent

### Claude Code (recommended)

Install the bundled plugin — MCP server + skill + slash commands + a `SessionStart` hook in one shot:

```bash
/plugin marketplace add SarthakShrivastav-a/pluckr
/plugin install pluckr@pluckr
/reload-plugins
```

What you get:

- **MCP server** with seven tools (search/get/list/outline + refresh/add/remove)
- **Skill** that tells Claude *when* to use the cache before reaching for the web
- **Slash commands** — `/pluckr-add`, `/pluckr-list`, `/pluckr-search`, `/pluckr-refresh`
- **`SessionStart` hook** — runs `pluckr list` so Claude knows what sources exist from message zero

Or manually wire just the MCP server:

```bash
claude mcp add pluckr -- pluckr mcp
```

### Cursor

Add to `~/.cursor/mcp.json`:

```json
{ "mcpServers": { "pluckr": { "command": "pluckr", "args": ["mcp"] } } }
```

### Claude Desktop

Add to `claude_desktop_config.json`:

```json
{ "mcpServers": { "pluckr": { "command": "pluckr", "args": ["mcp"] } } }
```

## MCP tool surface

| Tool | Purpose |
|---|---|
| `search_docs(query, sources?, limit?)` | BM25 search across subscribed sources. Returns chunks with heading path, snippet, freshness |
| `get_page(source, path)` | Full markdown of one cached page |
| `list_sources()` | Subscribed sources with kind, root, page count, last sync, stale flag |
| `get_outline(source)` | Heading tree of an entire source |
| `refresh_source(name)` | Re-run the pipeline for one source. *Mutating* |
| `add_source(spec, …)` | Subscribe a new source. *Mutating* |
| `remove_source(name, …)` | Drop a source from the registry. *Mutating* |

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
  registry.json                 # subscribed sources + freshness + auth refs
  sources/
    react.dev/
      pages/                    # markdown is the source of truth
        reference/hooks/useState.md
      manifest.json             # per-page hash, fetched_at, token_count
      index.db                  # SQLite FTS5 — rebuildable from pages/
```

Markdown files are the source of truth. Hand-edit them, run `pluckr reindex <source>`, and the FTS5 index catches up.

## Auth for private docs

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

## Architecture

Small Go interfaces, each implementation isolated in its own package:

```
fetch    →  render    →  chunk      →  store + retriever  →  mcp / cli
HTTP        HTML→md      heading-       FTS5 (modernc.org/      stdio
            with         bounded,       sqlite, no CGo)
            empty-       800-token
            content      cap
            detection
```

See [docs/design.md](docs/design.md) for the full v0.1 design — locked decisions, package boundaries, MCP tool semantics, sync model, and explicit out-of-scope items.

## Status

Active development. Source kinds, FTS5 search, the MCP read tools, and the CLI all work. Headless rendering for SPA-only docs sites and pluggable vector retrievers are designed-in but not yet built. Every push to `main` auto-publishes the next semver patch with binaries.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Bug reports, source-kind PRs, renderer improvements, and CLI / MCP polish are all welcome.

## License

[MIT](LICENSE).
