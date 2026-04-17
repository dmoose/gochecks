# gocheck (installed in this repo)

`gocheck` is wired into Claude Code here as a `PostToolUse` hook.
Every time Claude Code edits a `.go` file, gocheck runs in the
background before the next agent turn and does four things:

1. **Silently auto-applies `gofmt` + `goimports`** — the agent
   never sees whitespace noise.
2. **Runs `go build` + `go vet` per changed package** — compile
   and analysis errors surface before the agent's next action.
   Non-zero exit codes block the hook, preventing the agent from
   building on top of a broken state.
3. **Runs probed bug detectors** when their binaries are on PATH:
   `staticcheck`, `errcheck`, `ineffassign`. Silent when absent.
4. **Scope-filters findings** to the files the agent actually
   touched — no noise from unrelated packages.

## Why this is worth having

Without the hook, Claude runs `go vet` / `gofmt` itself, pastes
the raw stdout into conversation context, interprets it, edits,
re-runs. Each cycle is 1-3k tokens of raw output plus a
self-correction loop. With the hook:

- Formatting fixes are invisible — zero tokens spent.
- Remaining findings are classified (error / warning /
  suggestion) and tightly scoped.
- Compile-level errors block the hook, so the agent can't
  commit a half-broken state and then try to fix it turn by turn.
- On any non-trivial Go repo, this saves 10-50k tokens per
  session and eliminates multiple error-recovery loops.

## Installed by `gocheck install`

- `.claude/settings.json` — PostToolUse hook entry matching
  `Edit|Write|MultiEdit` on `*.go` files. Non-destructively
  merged with any pre-existing settings.
- `.claude/skills/go-current/SKILL.md` — primes Claude to write
  Go 1.26 idioms on the first pass so modernize has less to
  rewrite.
- `.claude/gocheck.md` — this file.
- `.gocheck.json` — optional; only if you ran `install --config`.

## Get the most out of it

### 1. Install the optional tools (all probed-for-present)

```bash
go install golang.org/x/tools/cmd/goimports@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/kisielk/errcheck@latest
go install github.com/gordonklaus/ineffassign@latest
```

Each adds a bug-detection layer. None is disruptively slow
(~50-200ms per package, Go's build cache handles transitive
invalidation correctly).

### 2. Drop a `.gocheck.json` at the repo root if you want tuning

```json
{
  "test": false,
  "test_timeout": "30s",
  "auto_fix": false,
  "linters": false,
  "exclude": ["*_gen.go", "testdata/"]
}
```

- `test: true` — runs `go test` on each changed package on every
  hook fire. Costs ~1s per package but gives real test signal on
  every edit. Leave off if your tests have side effects or slow
  setup.
- `auto_fix: true` — extends silent-apply from gofmt/goimports to
  Go 1.26's `go fix` modernizers, and to `golangci-lint --fix`
  when `linters` is also on.
- `linters: true` — enables the `golangci-lint` umbrella (100+
  linters). Adds ~0.5-2s cold startup. Respects your
  `.golangci.yml`. When enabled, gocheck takes ownership of
  `errcheck`/`ineffassign` coverage via the umbrella and skips
  its standalone probes.
- `exclude` — glob patterns dropped from `--changed` before any
  tool runs. Generated-file comments
  (`// Code generated ... DO NOT EDIT.`) are always skipped
  automatically.

### 3. If you enable `linters`, disable `modernize` in `.golangci.yml`

Go 1.26's `go fix` (always on in gocheck) and golangci-lint's
`modernize` linter wrap the same analyzers. Enabling both
double-reports the same diagnostics.

```yaml
# .golangci.yml
linters:
  disable:
    - modernize
```

### 4. Verify it's wired

```bash
gocheck status
```

Shows tool probes, loaded config, and the last 20 hook
invocations with duration and exit code. If the hook log stays
empty after you edit a `.go` file through Claude Code, the hook
isn't firing — see `HOOKS.md` in the gocheck repo for debug tips.

```bash
gocheck check --changed path/to/file.go
```

Dry-run a single file without going through the hook.

## State and observability

- `~/Library/Caches/gocheck/hook.log` — timestamped record of
  every hook invocation with duration, exit code, and finding
  count. Safe to delete anytime; recreated on next run.
- Exit codes: `0` = clean or suggestions only; `1` = at least one
  warning; `2` = at least one error (blocks the hook).

## Uninstall

1. Open `.claude/settings.json`, remove the PostToolUse entry
   whose `command` contains `gocheck check`. Keep other entries.
2. Delete `.claude/skills/go-current/` if you don't want the
   priming skill.
3. Delete this file and `~/Library/Caches/gocheck/` if you want
   to leave no trace.

No other state — gocheck doesn't modify `$GOCACHE` or any project
file beyond the silent gofmt/goimports rewrites that happen on
every edit.

## Upstream

See the gocheck repo for `DESIGN.md` (scope, non-goals,
roadmap) and `HOOKS.md` (full Claude Code integration reference
including advanced flags and exit-code semantics).
