# Changelog

## Unreleased

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
