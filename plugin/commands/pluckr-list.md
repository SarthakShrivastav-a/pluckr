---
description: List the docs sources subscribed in the local pluckr cache.
---

Call the MCP `list_sources` tool and render the result as a small table with these columns: name, kind, root, page count, last synced (relative time), stale.

Do not include the raw JSON. If a source is stale (>= 7 days), highlight it.
