# Changelog

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
