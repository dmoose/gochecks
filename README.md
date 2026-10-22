# gochecks

Runs the Go toolchain and the usual analyzers over the files you just
changed and reports only what matters, in a form a Claude Code hook can
hand straight back to the model.

`gochecks check` formats with gofumpt (or gofmt) and goimports, then
runs `go build`, `go vet` and, when installed, staticcheck, errcheck,
ineffassign and golangci-lint on the changed packages. Findings outside
the changed files are dropped. Each finding is one line with file,
position, class (`error`, `warning`, `suggestion`) and tool, as JSON
lines or text.

golangci-lint runs with the repo's `.golangci.yml` when there is one,
otherwise with the [profile](./.golangci.yml) built into gochecks:
the bugs vet and staticcheck miss, idiom fixes that apply mechanically,
and none of the style cops. Copy it into a repo to tune it there.

## Install

```sh
go install github.com/dmoose/gochecks@latest
```

Archives for macOS, Linux and Windows are on the
[releases page](https://github.com/dmoose/gochecks/releases).

## Quick start

```sh
cd your-repo
gochecks install --config
gochecks doctor
```

`install` adds `gochecks hook` to `.claude/settings.json` as a
PostToolUse hook on `Edit|Write`, writes the `go-current` skill and a
short `.claude/gochecks.md`, and with `--config` a default
`.gochecks.json`. `doctor` reports the toolchain and each analyzer's
version and health; `doctor --fix` installs or updates them.

## Commands

| Command | |
|---|---|
| `hook` | PostToolUse entry point; reads the event from stdin |
| `check [files]` | format, build, vet, analyzers; exit 0/1/2 by highest class |
| `fix [files]` | `check` plus `go fix` modernize |
| `show file:line` | source context and vet findings at a position |
| `doctor [--fix]` | toolchain and tool health |
| `status` | config in effect and the hook log tail |
| `install` | wire a repo; `--upgrade` refreshes what install wrote |

## Configuration

`.gochecks.json` at the repo root, all keys optional:

| Key | Default | |
|---|---|---|
| `test` | `false` | run `go test` on changed packages |
| `test_timeout` | `"30s"` | passed to `go test -timeout` |
| `auto_fix` | `false` | also apply `go fix` modernize when nothing blocks |
| `linters` | `true` | run golangci-lint when installed; it then owns staticcheck, errcheck and ineffassign |
| `exclude` | `[]` | patterns dropped before any tool runs |

Vendored and generated files are always skipped.

See [HOOKS.md](./HOOKS.md) for how the hook reports to the model and
[CHANGELOG.md](./CHANGELOG.md) for releases.

## License

[MIT](./LICENSE).
