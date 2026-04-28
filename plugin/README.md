# pluckr Claude Code plugin

Bundles the `pluckr` MCP server, a docs-cache skill, and four slash commands so Claude Code can use the local docs cache out of the box.

## Install

```bash
# 1. Install the pluckr binary.
go install github.com/SarthakShrivastav-a/pluckr/cmd/pluckr@latest

# 2. Install this plugin from inside Claude Code.
/plugin marketplace add SarthakShrivastav-a/pluckr
/plugin install pluckr@pluckr
/reload-plugins
```

## What you get

| Surface | Purpose |
| --- | --- |
| MCP server `pluckr` | Exposes search_docs, get_page, list_sources, get_outline, refresh_source, add_source, remove_source. |
| Skill `pluckr-docs-cache` | Tells Claude when to use the cache and how to format hits. |
| `/pluckr-add <spec>` | Subscribe to a new source. |
| `/pluckr-list` | Show subscribed sources with freshness. |
| `/pluckr-search <query>` | Search the cache and render top hits. |
| `/pluckr-refresh [name]` | Refresh one or every stale source. |
| `SessionStart` hook | Runs `pluckr list` so the agent has source awareness from message zero. |

## First run

```bash
pluckr add https://react.dev/reference --pull
pluckr add facebook/react/docs --pull
pluckr add ~/notes-vault --pull
```

Then ask Claude something like *"how do React effects clean up?"* — the agent should reach for the cache first.
