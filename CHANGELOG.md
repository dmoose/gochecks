# Changelog

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
