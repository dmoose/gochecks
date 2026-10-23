# gochecks

Go toolchain checks for Claude Code hooks. After every edit of a Go
file, gochecks formats it, builds and vets its package, runs the
analyzers you have installed, and hands back only the findings in the
files that changed, one line each. It also works from the shell and in
CI.

What runs, in order:

1. gofumpt (or gofmt when gofumpt is not installed) and goimports, applied
   in place. goimports groups the module's own imports separately.
2. `go build` and `go vet` on each changed package. A package that fails
   to build stops there; nothing else would add information.
3. golangci-lint when installed, with the repo's own config or the
   profile built into gochecks. Without golangci-lint: staticcheck,
   errcheck and ineffassign, each when installed.
4. `go test` on the changed packages, when enabled in config.
5. `go fix` modernize, when enabled in config or via `gochecks fix`.

Each finding carries file, line, column, a class and the tool:

```
scope.go:56:2 warning/golangci-lint/bodyclose: response body must be closed
```

Classes are `error` (build, vet, test failures), `warning` (bug
finders) and `suggestion` (idiom and formatting). Vendored files and
files marked `Code generated ... DO NOT EDIT` are skipped.

## Install

```sh
go install github.com/dmoose/gochecks@latest
gochecks doctor --fix
```

`doctor` lists the Go toolchain and each analyzer with its version,
whether it was built against the current Go, and whether a newer
release exists. `--fix` installs or updates goimports, gofumpt,
staticcheck, errcheck, ineffassign and golangci-lint. Run it again
after a Go upgrade: analyzers built against an older Go cannot read the
new export data, and until they are rebuilt gochecks reports each one
as a single warning instead of its findings.

Archives for macOS, Linux and Windows are on the
[releases page](https://github.com/dmoose/gochecks/releases).

## Set up a repo

```sh
cd your-repo
gochecks install --config
```

This writes:

- `.claude/settings.json`: `gochecks hook` as a PostToolUse hook on
  `Edit|Write` and as a SessionStart hook. Existing settings are kept.
- `.claude/skills/go-current/SKILL.md`: a skill that has the model
  write current Go idioms on the first pass, so there is less to fix.
- `.claude/gochecks.md`: a short note on what is wired and how to tune it.
- `.gochecks.json` (with `--config`): the defaults below, to edit.

`gochecks install --upgrade` refreshes those files after a gochecks
update. `gochecks install --lint-config` additionally writes the lint
profile as `.golangci.yml`; see Linting.

On each edit the hook returns errors to the model as a blocking
message and warnings and suggestions as added context. At session
start it adds one line when dependencies have newer releases.
[HOOKS.md](./HOOKS.md) has the details and the exact settings.

## Commands

| Command | |
|---|---|
| `hook` | hook entry point; reads the Claude Code event from stdin |
| `check [files]` | format, build, vet, analyzers; exit 0, 1 or 2 by highest class; `--no-apply` reports formatting instead of applying it |
| `fix [files]` | `check` with `go fix` modernize first |
| `show file:line` | source context and vet findings at a position |
| `doctor [--fix]` | toolchain and analyzer health; `--fix` installs or updates |
| `deps [--fail]` | direct modules, copied assets and workflow pins with a newer release; `--fail` exits 1 when any |
| `status` | config in effect and the hook log tail |
| `install` | wire a repo; `--upgrade`, `--config`, `--lint-config` as above |
| `lint-config` | print the built-in golangci-lint profile |
| `version` | version, from the release build or the module |

`gochecks help` lists the flags. Output is JSON lines by default and
`--text` gives one finding per line; the hook uses neither and speaks
Claude Code's hook protocol.

## Configuration

`.gochecks.json` at the repo root. Every key is optional; this is the
default:

```json
{
  "test": false,
  "test_timeout": "30s",
  "auto_fix": false,
  "linters": true,
  "exclude": [],
  "assets": {}
}
```

- `test`: run `go test` on each changed package. Off by default because
  test suites vary in cost; on a small package it adds a second or two
  per edit. Failures are errors, with the file and line of the first
  `t.Errorf`.
- `test_timeout`: passed to `go test -timeout`.
- `auto_fix`: also apply `go fix` modernize to the changed packages when
  the check found nothing blocking, and run golangci-lint with `--fix`.
  Fixes that modernize could not apply because two of them overlap are
  reported as suggestions at their position.
- `linters`: set `false` to keep golangci-lint out even when installed.
- `exclude`: patterns dropped before any tool runs. A pattern with a
  `/` matches anywhere in the path (`"testdata/"`, `"internal/gen/"`);
  one without matches the file name (`"*_gen.go"`, `"mock_*.go"`).
- `assets`: files copied from npm packages, for `deps`. See
  Dependencies.

## Linting

golangci-lint runs when it is installed. Which configuration it uses:

1. The repo's own `.golangci.yml` (or `.yaml`, `.toml`, `.json`), found
   from the package directory up to the module root.
2. Otherwise the profile built into gochecks, which is this repo's
   [`.golangci.yml`](./.golangci.yml).

The profile enables the standard five (errcheck, govet, ineffassign,
staticcheck, unused), a set of bug finders that vet and staticcheck do
not cover (among them bodyclose, errorlint, exhaustive, gocritic,
gosec, nilerr, rowserrcheck, sqlclosecheck, unparam) and a set of
idiom linters that mostly carry autofixes (copyloopvar, intrange,
perfsprint, usestdlibvars, usetesting, revive with a fixed rule list,
and others). It leaves out style linters that argue about blank lines,
line length, function length or magic numbers, the modernize linter
because `go fix` already runs it, and noctx because it flags every
`exec.Command` in a command-line tool. gosec's file-permission and
unhandled-error rules are excluded for the same reasons.

Findings from the idiom linters are suggestions; the rest are warnings,
or errors when golangci-lint itself says so. With `auto_fix` on, the
autofixes are applied before anything is reported.

To adopt the profile in a repo and tune it there:

```sh
gochecks install --lint-config   # writes .golangci.yml, never overwrites
gochecks lint-config             # prints it
```

Once a repo has its own config, that config is used as is. If you keep
one, leave the modernize linter off in it.

When golangci-lint runs it owns staticcheck, errcheck and ineffassign,
so those are not run again on their own.

## Dependencies

`gochecks deps` reports direct dependencies that have a newer release.

Go modules come from `go list -m -u`. Indirect dependencies are left
out. Modules sharing the main module's host and owner (your own) are
listed first.

Copied assets are files you vendor from npm packages, such as an
`htmx.min.js` in `static/`. Name them in `assets`, path relative to
`.gochecks.json`, mapped to the npm package:

```json
{
  "assets": {
    "static/htmx.min.js": "htmx.org",
    "static/echarts.min.js": "echarts"
  }
}
```

The current version is read from the file's header (a `version:` or
`vX.Y.Z` marker, else the first dotted version in the first 8 KiB) and
compared with the package's latest version on the npm registry. A file
with no readable version is listed as `unknown`.

```
module github.com/mark3labs/mcp-go v0.54.0 -> v1.3.0
asset static/htmx.min.js 2.0.4 -> 2.0.11
```

Workflow pins come from `.github/workflows/*.yml`: `uses: owner/repo@v5`
against the action's latest release (a major-only pin is behind only
when a newer major exists), golangci-lint-action's `version:` against
the latest golangci-lint, and `go install pkg@vX.Y.Z` for the tools
gochecks knows. Pins by commit hash are left alone.

```
workflow actions/setup-go (ci.yml) v5 -> v6.1.0
workflow golangci-lint (go.yml) v2.12.2 -> v2.15.0
```

`deps` exits 0 by default; `--fail` exits 1 when anything is behind,
for CI or a `make check` that should fail. The SessionStart hook runs
the same check and adds one line of context when something is behind,
and stays silent on network trouble.

## Not in scope

gochecks orchestrates tools; it has no analyzers of its own, no custom
lint rules, and no language server. Semantic queries belong to gopls.

## License

[MIT](./LICENSE).
