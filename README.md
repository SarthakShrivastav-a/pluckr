# pluckr

Pull docs sites, GitHub repos, `llms.txt` endpoints, and local folders into a
local markdown cache that any LLM agent can search via MCP.

```
$ pluckr add https://react.dev/reference
  added react.dev (412 pages, 18.4s)

$ pluckr search "useEffect cleanup"
  react.dev › Reference › useEffect › Cleaning up an effect
  react.dev › Learn › You Might Not Need an Effect

$ pluckr mcp     # serve the cache as an MCP tool to Claude Code, Cursor, etc.
```

## Why

LLM agents currently choose between:
- dumping entire docs sites into context (wasteful, expensive, fragile),
- browsing live (slow, flaky, blocked by SPAs),
- hoping their training data is fresh (it isn't).

`pluckr` is a local-first, agent-native docs cache. One static binary keeps a
folder of clean markdown plus a tiny SQLite full-text index. An MCP server
exposes that cache to any compatible host (Claude Code, Cursor, Claude
Desktop, Cline, Zed, ...).

## Status

Early. Targeting a v0.1 with the four core source types (website, `llms.txt`,
GitHub, local), HTTP fetching, FTS5 search, and a read-only MCP server.

## Install

```bash
go install github.com/SarthakShrivastav-a/pluckr/cmd/pluckr@latest
```

(Homebrew, Scoop, and a Claude Code plugin bundle are planned for the first
tagged release.)

## License

MIT. See [LICENSE](LICENSE).
