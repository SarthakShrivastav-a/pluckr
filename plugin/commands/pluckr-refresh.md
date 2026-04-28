---
description: Refresh one or all stale pluckr sources.
---

If the user provided a source name, call the MCP `refresh_source` tool with that name. Otherwise:

  1. Call `list_sources`.
  2. Identify any source where `stale=true`.
  3. Call `refresh_source` for each one in parallel order.
  4. Report a one-line summary per source: `name: pages, chunks, errors, elapsed`.

Source: !ARGUMENTS
