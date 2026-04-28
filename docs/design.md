# pluckr design (v0.1)

## What it is

A single Go binary that ingests public documentation, GitHub repositories,
`llms.txt` endpoints, and local folders into a local markdown cache, indexes
the content with SQLite FTS5, and serves the result to LLM agents over MCP.

The pitch is "your agent's persistent, curated, always-fresh docs brain" -
local-first, no API keys required, works offline, drops into Claude Code /
Cursor / any MCP host.

## Locked decisions

| Dimension | Choice |
| --- | --- |
| Use case | LLM context / RAG pipeline |
| Form factor | Files on disk + MCP server (`pluckr mcp`) |
| Scope | Multi-source knowledge base (registry of subscribed sources) |
| Fetching | HTTP-first, auto-escalate to headless when content looks empty |
| Retrieval | FTS5 / BM25 default, pluggable `Retriever` interface for vectors later |
| Sources (v0.1) | `website`, `llms_txt`, `github`, `local` |
| Storage | Flat markdown files + sidecar `index.db` + `registry.json` |
| Chunking | Heading-bounded with 800-token soft cap, full heading path retained |
| Sync | Opportunistic background refresh on MCP-server start, no daemon |
| MCP surface | `search_docs`, `get_page`, `list_sources`, `get_outline`, `refresh_source`, `add_source`, `remove_source` (mutations gated behind config) |
| Auth | Generic header / cookie passthrough, env-var interpolation |
| Distribution | GoReleaser → GitHub Releases + Homebrew tap + Scoop bucket; Claude Code plugin bundle; npm wrapper |

## On-disk layout

```
~/.pluckr/
  registry.json         # subscribed sources, last_synced_at, refresh policy, auth refs
  config.toml           # global settings (mcp.allow_add_source, etc.)
  sources/
    react.dev/          # one folder per source (slug derived from source name)
      pages/            # flat markdown mirroring URL / repo paths
        reference/hooks/useState.md
      index.db          # sidecar SQLite FTS5, fully rebuildable from pages/
      manifest.json     # per-page hash, last_seen, fetch metadata
```

Markdown is the source of truth. `index.db` is a derived artifact;
`pluckr reindex` regenerates it from the markdown files at any time.

## Package layout

```
github.com/SarthakShrivastav-a/pluckr
├── cmd/pluckr            # main entry, CLI dispatch
├── internal/
│   ├── source            # Source interface + impls (website, llms_txt, github, local)
│   ├── fetch             # Fetcher interface + impls (http, headless)
│   ├── render            # HTML → markdown
│   ├── chunk             # Heading-bounded chunker with token cap
│   ├── store             # SQLite store + page filesystem
│   ├── retriever         # Retriever interface + FTS5 impl
│   ├── registry          # registry.json read/write
│   ├── pipeline          # discover → fetch → render → chunk → index orchestration
│   ├── mcp               # MCP server (tool definitions + handlers)
│   ├── config            # config schema and loader
│   └── ui                # CLI progress UI
└── docs                   # design and reference docs
```

## Key abstractions

```go
type Source interface {
    Kind() string
    Name() string
    Discover(ctx) (<-chan Page, error)   // streams Pages: URL + raw bytes + content-type
}

type Fetcher interface {
    Fetch(ctx, url, opts) (*Response, error)
}

type Renderer interface {
    Render(raw []byte, contentType, url string) (Document, error)  // returns clean markdown + title + outline
}

type Chunker interface {
    Chunk(doc Document) []Chunk    // heading-bounded, token-capped, with heading_path
}

type Retriever interface {
    Index(ctx, source string, chunks []Chunk) error
    Search(ctx, query string, opts) ([]Hit, error)
}
```

The `pipeline` package wires them together, runs N workers per source, and
writes pages + chunks atomically.

## Worker pool

Per-source: `runtime.NumCPU() * 2` goroutines (minimum 8). Bounded queue.
HTTP-first; if the renderer reports "looks empty" (heuristic in `render`),
the page is requeued through the headless fetcher (when available). Single
500ms-ish per-page Defuddle-equivalent timeout, fall back to a `<main>` /
`<article>` / `<body>` text dump so we never lose a page entirely.

## MCP tools

All tools take and return JSON. Read tools are always on; mutating tools
(`refresh_source`, `add_source`, `remove_source`) are gated by
`mcp.allow_mutations` in `config.toml`. The MCP host's own tool-call consent
gate is the user's last line of defense for `add_source`.

```
search_docs(query, sources?, kind?, limit=10)  → []Hit
get_page(source, path | url)                   → Document
list_sources()                                 → []SourceInfo
get_outline(source)                            → []HeadingNode
refresh_source(name)                           → RefreshSummary
add_source(spec)                               → SourceInfo   # gated
remove_source(name)                            → ok           # gated
```

Each `Hit` carries `source`, `url`, `heading_path`, `snippet`,
`token_count`, `last_synced_at`, `stale`. Agents see freshness inline.

## Sync semantics

No always-on daemon. When the MCP server starts:

1. Read `registry.json`.
2. For each source whose `last_synced_at` is older than its `refresh`
   interval (default `7d`, can be `manual` or `never`), spawn a goroutine
   that re-runs the source's pipeline and updates files + index.
3. Continue serving the existing index immediately.

The Claude Code plugin's `SessionStart` hook calls `pluckr refresh --due`
which delegates to this same code path.

## Auth

Source config supports `headers: {Key: Value}` and `cookies: {Name: Value}`
blocks. Values can reference env vars: `Authorization: "Bearer ${MY_TOKEN}"`.
Resolution happens at fetch time, not write time, so secrets never enter the
registry file.

OAuth, token refresh, and provider-specific integrations (Notion API, etc.)
are explicitly out of scope for v0.1.

## Distribution

| Channel | Bundle |
| --- | --- |
| GitHub Releases | platform binaries via GoReleaser |
| Homebrew tap | `brew install SarthakShrivastav-a/tap/pluckr` |
| Scoop bucket | `scoop install pluckr` (Windows) |
| `go install` | `github.com/SarthakShrivastav-a/pluckr/cmd/pluckr@latest` |
| Claude Code plugin | bundles MCP wiring, `/pluckr` slash command, `pluckr` skill |
| npm | thin wrapper that downloads the matching binary |

## Out of scope for v0.1

- Headless rendering (interface defined, no impl yet)
- Vector retriever (interface defined, FTS5 only at v0.1)
- OAuth and provider-specific auth flows
- PDF, OpenAPI, YouTube transcript sources
- Always-on daemon mode
- Cloud / multi-tenant operation

## Risks and mitigations

- **`add_source` lets an agent scrape arbitrary URLs.** Mitigations:
  the host's tool-call consent gate, an opt-in `mcp.allow_mutations` flag,
  optional `mcp.domain_allowlist` regex, and writes scoped to
  `~/.pluckr/sources/`.
- **A single SPA-heavy site silently produces empty pages.** The renderer
  emits an `EmptyContent` warning so the CLI surfaces it, and the headless
  fetcher slot exists from day one to fix it later.
- **The FTS5 index drifts from disk.** `pluckr reindex` rebuilds it from
  `pages/` deterministically; the markdown is the source of truth.
