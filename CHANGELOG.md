# Changelog

Notable changes per release. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Added

- Initial v0.1 implementation:
  - Four source kinds: `website`, `llms_txt`, `github`, `local`.
  - HTTP fetcher with configurable headers, cookies, and `${ENV}` expansion.
  - HTML → markdown renderer with native-markdown fast path and empty-content detection.
  - Heading-bounded chunker with an 800-token soft cap.
  - SQLite FTS5 retriever via `modernc.org/sqlite` (no CGO).
  - On-disk cache: flat markdown + per-source manifest + sidecar `index.db`.
  - Subscription registry at `~/.pluckr/registry.json`.
  - Pipeline that wires source → render → chunk → store → index.
  - Cobra-based CLI: `add`, `list`, `remove`, `pull`, `search`, `reindex`, `mcp`, `root`.
  - MCP server with seven tools: `search_docs`, `get_page`, `list_sources`, `get_outline`, `refresh_source`, `add_source`, `remove_source`.
  - Claude Code plugin scaffold: MCP server registration, docs-cache skill, four slash commands, SessionStart hook.
  - GoReleaser config for per-platform binaries published to GitHub Releases. Homebrew tap and Scoop bucket plumbing is wired but gated behind a repo variable for future activation.
  - GitHub Actions workflows: `ci.yml` (vet + race tests on Linux / macOS / Windows) and `auto-release.yml` (auto-bump and publish on every push to `main`).
