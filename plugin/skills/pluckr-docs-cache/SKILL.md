---
name: pluckr-docs-cache
description: Use whenever the user mentions a library, framework, SDK, or documented system to first search the local pluckr cache via the search_docs tool before reaching for the web. The cache holds curated, up-to-date markdown for the user's subscribed sources and is the cheapest, most accurate context for documented APIs.
---

# Using the pluckr docs cache

The user has a local pluckr cache running as an MCP server. It holds
markdown copies of the docs sites, GitHub repos, llms.txt endpoints,
and local folders they care about, indexed for full-text search.

## When to reach for it

- Any question about a library, framework, SDK, CLI, or service the
  user might have subscribed to (think React, Tailwind, Prisma, an
  internal SDK, an API reference).
- Questions that mention "the docs" or "the spec" - the user usually
  means *their* curated copy.
- Before writing fresh code that calls an API, look up the relevant
  symbols so you use current signatures.

## How to use it

1. Call `list_sources` once at the start of a session if you don't yet
   know what's cached. Cache the result; sources don't change mid-task.
2. Call `search_docs(query, sources?, limit?)` with the most specific
   identifier the user mentioned (function name, error message, config
   key). BM25 rewards exact matches.
3. If a hit is interesting, call `get_page(source, path)` to read the
   surrounding context. Don't dump the whole page into your reply
   verbatim - quote the relevant lines and link to the URL.
4. Note the `stale` flag on hits. If a result is older than seven
   days, mention that the cache is stale and consider whether the
   user wants `refresh_source(name)` or a web check.

## When not to use it

- General programming concepts ("how do closures work") - that is not
  what a docs cache is for.
- Code in the user's own repo - read the files directly.
- Anything where the user explicitly asks for live web information.
