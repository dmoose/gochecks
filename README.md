# gochecks

Context-efficient Go toolchain dispatcher for LLM/agent workflows.

Runs the standard Go toolchain (`gofmt`, `goimports`, `go build`,
`go vet`, `go fix` modernize) plus probe-for-present bug detectors
(`staticcheck`, `errcheck`, `ineffassign`) and an optional
`golangci-lint` umbrella over a set of changed files, and returns a
**structured, change-scoped, deduplicated** summary instead of raw
tool stdout. The `hook` subcommand is a Claude Code `PostToolUse` hook:
it reads the edit event from stdin and hands findings back through
the channels the model actually sees.

**Status:** v0.7-dev. `hook` / `check` / `fix` / `show` / `status` /
`doctor` / `install` live. Package-level parallelism, `.gochecks.json` config, opt-in
`golangci-lint` umbrella, embedded `go-current` skill, one-command
scaffold (hook + skill + install-time doc) via `gochecks install`.
See [DESIGN.md](./DESIGN.md) for scope and [HOOKS.md](./HOOKS.md)
for Claude Code integration.

## Quick start

```bash
go install github.com/dmoose/gochecks@latest
cd path/to/your/go/repo
gochecks install --config     # scaffold hook + skill + config
gochecks doctor               # toolchain + analyzer health; --fix installs
```

## Why

Raw toolchain output is expensive in LLM context. A single `go vet ./...`
plus `gofmt -d` plus `go fix -diff ./...` can dump kilobytes of text — most
of it either pre-existing (unrelated to the agent's change) or redundant
across tools. `gochecks` wraps those tools with a change-scope filter, a
classification scheme, and an auto-apply path for safe fixes, so the hook
returns a tight summary of what needs attention.

## Non-goals

- Not a replacement for `gopls`, `go fix`, `staticcheck`, or `ast-grep`
- Not a language server
- Not a reimplementation of analysis passes

## Install

```bash
go install github.com/dmoose/gochecks@latest
```

Release archives for macOS, Linux and Windows are on the
[releases page](https://github.com/dmoose/gochecks/releases).

## License

[MIT](./LICENSE).
