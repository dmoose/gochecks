# Changelog

## 0.10.0 (2026-11-21)

- A tool that checks nothing is a finding, never a clean pass. A module
  the enclosing `go.work` does not list is an `error/build` naming the
  missing `use` line; an analyzer that cannot start, or exits non-zero
  with nothing parseable, is a warning with its output.
- A file that does not parse is an error at its position; before, the
  formatter's failure made the hooks exit non-blocking.
- Build errors are reported from every file of a changed package, so a
  change that breaks a caller in an unchanged file blocks. A package
  that does not compile is named as not checked by vet and the linters.
- `install --stop` wires a Stop hook: errors or warnings in any Go file
  changed since HEAD, Bash edits included, keep the agent working.
  Suggestions come with the next prompt. A stuck agent is let through
  once and reminded on every prompt and the next session until a stop
  passes. `install --stop` also denies `gochecks allow` to the agent.
- `gochecks allow "<reason>"` releases one Stop block for the same
  findings. Grants are logged and listed by `audit`.
- Where the Stop hook is wired, session start tells the agent what
  gochecks runs for it and what is left to it. The first block in a
  session explains; later ones are one line.
- `gochecks audit` lists everything that turns a check off or pins
  something: `//nolint` and `//lint:ignore` with reasons, golangci-lint
  config departures from the profile, `.gochecks.json` settings, test
  off in a module with tests, `GOWORK=off`, `replace`, dependencies
  behind, a `make check` without gochecks, and Stop releases. Session
  start adds one line of counts.
- The lint profile enables nolintlint: a suppression names its linter
  and says why, and one that suppresses nothing is a warning.
- `doctor` exits 1 on wiring drift where it runs: no hook, the old
  `gocheck` name, or a `make check` without gochecks. `--require-stop`
  adds a missing Stop hook. `doctor --scan <dir>` lists every unwired
  module or workspace under `dir`, skipping what git ignores.
- `"linters": true` with golangci-lint missing is a warning.
- A golangci-lint govet finding that repeats go vet's is dropped.
- `check --text` prints paths relative to the working directory.

## 0.9.0 (2026-11-16)

- A tool that cannot read the current Go's export data is reported once
  per run whatever the changed files, naming the Go it was built with;
  `check` and the hook no longer report a clean run.
- `doctor` installs tools with the Go that builds the current module
  (`GOTOOLCHAIN`), names any still broken after `--fix`, and exits by the
  re-check. If that Go cannot be read, it says why and `--fix` installs
  nothing.
- `doctor` shows a newer Go patch as "pinned by go.mod" (or go.work), not
  as attention, when the module pins the exact Go running.
- `fix` and `auto_fix` modernize only the files given, not the rest of
  their packages, and format what `go fix` changed.
- `check` lists each `go fix` modernize rewrite in the changed files as a
  suggestion, so what `fix` would change shows first.
- `check --no-apply` writes nothing, even with `auto_fix`: modernize
  rewrites and golangci-lint fixes are reported instead.
- `check` and `fix` name each file that does not exist and exit 2, after
  checking the files that do.
- A finding about a whole file no longer prints a trailing "/".

## 0.8.2 (2026-10-24)

- `deps` also checks version pins in `.github/workflows`: action refs,
  golangci-lint-action's `version:`, and `go install` of known tools.
- golangci-lint runs with `--allow-parallel-runners`; its formatter
  findings are dropped since gochecks runs the formatters itself.

## 0.8.1 (2026-10-23)

- `deps`: direct Go modules and copied assets with a newer release, own
  modules first; `--fail` for CI. Assets come from an `assets` map in
  `.gochecks.json` (file path to npm package).
- `install` wires a SessionStart hook; the hook mentions dependencies
  that are behind when a session opens.

## 0.8.0 (2026-10-22)

- golangci-lint runs whenever installed, with the repo's `.golangci.yml`
  or the profile embedded in gochecks; idiom linters report as
  suggestions, bug linters as warnings; it then owns staticcheck,
  errcheck and ineffassign. `"linters": false` opts out.
- gofumpt replaces gofmt when installed; goimports groups the module's
  own imports with `-local`.
- doctor knows gofumpt.
- `install --lint-config` writes the profile as `.golangci.yml` when the
  repo has no golangci config; `lint-config` prints it.

## 0.7.0

- `hook` subcommand for Claude Code PostToolUse: event on stdin, errors
  to stderr with exit 2, warnings and suggestions as additionalContext.
- `doctor` subcommand: toolchain and analyzer versions, typecheck
  health, hook wiring; `--fix` installs and updates.
- JSON output parsed from go vet, go build, go fix, staticcheck and
  golangci-lint; modernize conflicts reported per diagnostic.
- Analyzers that cannot read the current toolchain's export data are
  reported once instead of silently dropped.
- Renamed from gocheck. Module path `github.com/dmoose/gochecks`,
  config file `.gochecks.json`.
- Go 1.27; go-current skill updated for 1.27.
- MIT licence, CI, release builds.
