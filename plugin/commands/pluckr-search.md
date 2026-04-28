---
description: Search the local pluckr docs cache and surface the top hits.
---

Use the MCP `search_docs` tool with the query the user provided. Show the top 5 hits with:

  - `source › heading_path`
  - the snippet, with the matched terms preserved
  - the URL

If any hit is flagged `stale`, mention it and offer to run `refresh_source` for that source. Do not dump full pages unless the user asks.

Query: !ARGUMENTS
