# gochecks (installed in this repo)

`gochecks` is wired into Claude Code here as a `PostToolUse` hook.
Every time Claude Code edits a `.go` file, gochecks runs in the
background before the next agent turn and does four things:

1. **Silently applies `gofumpt` (or `gofmt`) + `goimports`**, the
   agent never sees whitespace noise.
2. **Runs `go build` + `go vet` per changed package**, compile
   and analysis errors come back to the agent on stderr with exit
   2, before its next action. Warnings and suggestions arrive as
   `additionalContext` beside the tool result.
3. **Runs probed bug detectors** when their binaries are on PATH:
   `staticcheck`, `errcheck`, `ineffassign`. Silent when absent.
4. **Scope-filters findings** to the files the agent actually
   touched, no noise from unrelated packages.

## Why

Without the hook, the model runs `go vet` and `gofmt` itself and reads
the raw output back into its context, then fixes and re-runs. With the
hook, formatting is invisible, the remaining findings are classified
and scoped to the edit, and compile errors reach the model before its
next action.

## Installed by `gochecks install`

- `.claude/settings.json`, `gochecks hook` as a PostToolUse entry on
  `Edit|Write` and as a SessionStart entry that mentions
  dependencies with a newer release. The hook reads the event from stdin and ignores
  non-Go files itself. Non-destructively merged with any
  pre-existing settings.
- `.claude/skills/go-current/SKILL.md`, primes Claude to write
  Go 1.27 idioms on the first pass so modernize has less to
  rewrite.
- `.claude/gochecks.md`, this file.
- `.gochecks.json`, optional; only if you ran `install --config`.
- `.golangci.yml`, optional; only if you ran `install --lint-config`.

## Get the most out of it

### 1. Install the optional tools (all probed-for-present)

```bash
gochecks doctor --fix
```

Installs or updates `goimports`, `gofumpt`, `staticcheck`, `errcheck`,
`ineffassign` and `golangci-lint`. Each adds
a bug-detection layer at ~50-200ms per package. Run `gochecks doctor`
again after a Go upgrade: analyzers built against an older Go stop
reading the new export data, and the hook reports them as a single
warning until they are rebuilt.

### 2. Drop a `.gochecks.json` at the repo root if you want tuning

```json
{
  "test": false,
  "test_timeout": "30s",
  "auto_fix": false,
  "linters": false,
  "exclude": ["*_gen.go", "testdata/"]
}
```

- `test: true`, runs `go test` on each changed package on every
  hook fire. Costs ~1s per package but gives real test signal on
  every edit. Leave off if your tests have side effects or slow
  setup.
- `auto_fix: true`, extends silent-apply from gofmt/goimports to
  the `go fix` modernizers, and to `golangci-lint --fix`
  when `linters` is also on.
- `linters: false`, turns golangci-lint off. On by default when it
  is installed: the repo's `.golangci.yml` if present, otherwise
  the profile embedded in gochecks (bug finders plus autofix idiom
  linters, no style cops). It then owns `staticcheck`, `errcheck`
  and `ineffassign`.
- `exclude`, glob patterns dropped from `--changed` before any
  tool runs. Generated-file comments
  (`// Code generated ... DO NOT EDIT.`) are always skipped
  automatically.
- `assets`, copied files to check with `gochecks deps`, as
  `"static/htmx.min.js": "htmx.org"`: path relative to this file,
  npm package it came from.

### 3. Keeping your own `.golangci.yml`

Leave its `modernize` linter off: `go fix` modernize (always on in
gochecks) already covers it. To start from the gochecks profile, run
`gochecks install --lint-config`; it writes `.golangci.yml` only when
the repo has no golangci config, and never overwrites one.

### 4. Verify it's wired

```bash
gochecks doctor
gochecks status
```

`doctor` shows the toolchain, each tool's health, and whether this
repo's hook is wired. `status` shows the loaded config and the last
20 hook invocations with duration and exit code. If the hook log stays
empty after you edit a `.go` file through Claude Code, the hook
isn't firing, see `HOOKS.md` in the gochecks repo for debug tips.

```bash
gochecks check --changed path/to/file.go
```

Dry-run a single file without going through the hook.

## State and observability

- `~/Library/Caches/gochecks/hook.log`, timestamped record of
  every hook invocation with duration, exit code, and finding
  count. Safe to delete anytime; recreated on next run.
- Hook exit codes: `0` = clean, or warnings/suggestions delivered
  as `additionalContext`; `2` = at least one error, delivered on
  stderr; `1` = hook misconfigured (shown to the user, not the
  model). `gochecks check` from the shell keeps 0 / 1 / 2 by
  highest class.

## Uninstall

1. Open `.claude/settings.json`, remove the PostToolUse entry
   whose `command` is `gochecks hook`. Keep other entries.
2. Delete `.claude/skills/go-current/` if you don't want the
   priming skill.
3. Delete this file and `~/Library/Caches/gochecks/` if you want
   to leave no trace.

No other state, gochecks doesn't modify `$GOCACHE` or any project
file beyond the silent gofmt/goimports rewrites that happen on
every edit.

## Upstream

See `HOOKS.md` in the gochecks repo for the full integration reference.
