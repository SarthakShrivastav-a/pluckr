# Contributing to pluckr

Bug reports, source-kind PRs, renderer improvements, and CLI / MCP polish are all welcome. This file keeps the bar low and the loop tight.

## Setup

```bash
git clone https://github.com/SarthakShrivastav-a/pluckr.git
cd pluckr
go test ./...
go build -o bin/pluckr ./cmd/pluckr
./bin/pluckr --help
```

Go 1.25 or later is required; the build pulls in modernc.org/sqlite (pure Go) so CGO is never needed.

## Branching

One small change per branch. Branch names follow `feat/<topic>`, `fix/<topic>`, or `docs/<topic>`.

```bash
git checkout -b feat/source-pdf
# ... commits ...
git push -u origin feat/source-pdf
gh pr create
```

PRs against `main`. Keep commits scoped: a feature commit, a test commit, and a docs commit is fine; a thousand-line dump that does ten things isn't.

## Tests

Every package has a `_test.go` file next to it. Go style: table-driven tests, `httptest` for HTTP-touching code, `t.TempDir()` for filesystem code.

```bash
go test ./...           # everything
go test -race ./...     # what CI runs
go test -run TestName ./internal/source/website
```

## Layout cheatsheet

```text
cmd/pluckr/         entrypoint
internal/types      shared value types (no other internal deps)
internal/fetch      Fetcher interface + http impl
internal/render     HTML / native-markdown -> Document
internal/chunk      heading-bounded chunker with token cap
internal/store      page filesystem + manifest + path math
internal/registry   subscribed-sources file
internal/retriever  Retriever interface + fts5 impl
internal/source     Source interface + helpers
internal/source/{website,llms_txt,github,local}  source kinds
internal/pipeline   wires every above package together
internal/cli        cobra commands
internal/mcp        MCP server (registers itself with cli)
plugin/             Claude Code plugin scaffold
docs/               design + reference
```

The dependency arrow runs left-to-right: types is the leaf, pipeline / cli / mcp sit at the top.

## Adding a source kind

1. Add the kind constant and validation case in `internal/registry/registry.go`.
2. Implement the `source.Source` interface in `internal/source/<kind>/`.
3. Wire it into `pipeline.NewSource`.
4. Teach `cli.detectKind` and `cli.deriveName` about the new spec shape if it isn't covered already.
5. Add a SKILL/command/hook entry in the plugin if it changes the user-facing flow.

## Coding style

```bash
gofmt -s -w .
go vet ./...
```

Comments explain *why*, not *what*. Identifier names should already tell the reader what the code does.

## Releasing

Tag with `git tag vX.Y.Z && git push --tags`; the release workflow runs goreleaser, publishes binaries, and updates the brew tap and scoop bucket if `PLUCKR_RELEASE_TOKEN` is configured.
