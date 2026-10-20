# gochecks — Claude Code hook integration

How to wire `gochecks` into Claude Code as a `PostToolUse` hook so that
every agent-driven edit of a `.go` file is mechanically sanitized
before the next turn.

## Install at project scope, not user scope

Install the hook at `<repo>/.claude/settings.json` — **never** at
`~/.claude/settings.json`. Language-gated tooling belongs with the
repo it serves. User scope would fire gochecks inside TypeScript,
markdown, Xcode, etc. repos and either waste cycles or error out.

## Canonical `settings.json`

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          { "type": "command", "command": "gochecks hook" }
        ]
      }
    ]
  }
}
```

`gochecks install` writes exactly this, merging into any existing
settings. The hook subcommand reads the PostToolUse event from stdin,
takes `tool_input.file_path`, and returns silently for anything that
is not a Go source file, so no shell glue is needed. Hooks inherit
the parent environment; `gochecks` must be on that PATH (`make
install` puts it on `$GOPATH/bin`).

For a live example see [`.claude/settings.json`](./.claude/settings.json)
in this repo.

## What the model sees

Claude Code only surfaces two channels from a PostToolUse hook: stderr
on exit 2, and a JSON object on stdout on exit 0. Plain stdout on
exit 0 goes to the debug log, and exit 1 shows the user a notice the
model never reads. `gochecks hook` maps findings onto those channels:

| Findings                    | Channel                                   | Exit |
|-----------------------------|-------------------------------------------|------|
| none                        | nothing                                   | 0    |
| warnings / suggestions only | `hookSpecificOutput.additionalContext`    | 0    |
| any `error`                 | text on stderr                            | 2    |
| hook misconfigured          | message on stderr (user sees it)          | 1    |

Both report forms are the same compact text: one line per finding,
paths relative to the session `cwd`:

```
gochecks: 2 finding(s)
pkg/a.go:3:4 warning/staticcheck: unused variable x
pkg/a.go:9:2 suggestion/modernize: for i := 0; i < n; i++ → for i := range n
```

The `check` subcommand keeps its own exit codes (0 / 1 / 2 by highest
class) for shell and CI use.

## Silent mid-turn file rewrites

`gochecks check` auto-applies `gofmt -s -w` and `goimports -w` to the
changed files before reporting anything. This means the hook can
**rewrite a file the agent just wrote**, mid-turn.

Claude Code's harness tolerates this: the in-context view of a file
is refreshed on the next `Read` / `Edit` targeting it, and `Edit`
failures from stale contents surface with a clear error. Don't
disable auto-apply unless you have a specific reason; the whole
point is to save the agent from spending tokens re-reading
formatting diffs.

If you want a strict "report, don't rewrite" mode for CI or
read-only contexts, use `--no-apply`: formatting becomes a
`suggestion`-class finding instead of a silent rewrite.

## Excluded files

`gochecks` filters its own input before running any tool. The
following are skipped unconditionally in `--changed`:

- Files not ending in `.go`
- Directories
- Missing files (tool may have deleted them)
- Any path containing a `vendor/` segment
- Files whose header line matches `// Code generated ... DO NOT EDIT.`
  anywhere before the `package` clause

Repo-specific patterns go in `.gochecks.json` under `exclude`.

## Optional tools

`gochecks` probes for these at runtime and uses them when present;
absent ones are silently skipped. `gochecks doctor` reports which are
installed, whether they still work against the current Go toolchain,
and whether a newer release exists; `doctor --fix` installs them:

- `staticcheck` — extended static analysis, class `warning`.
- `errcheck` — unchecked error returns, class `warning`.
- `ineffassign` — wasted assignments, class `warning`.
- `golangci-lint` — umbrella of 100+ linters. Opt-in only: enable
  via `"linters": true` in `.gochecks.json`. Off by default because
  of its 0.5-2s cold startup.

### Interaction with `golangci-lint`

When `linters: true` and `golangci-lint` is on PATH:

- gochecks defers `errcheck` and `ineffassign` coverage to the
  umbrella (the umbrella runs both, so gochecks does not to avoid
  double-reporting).
- With `auto_fix: true`, gochecks additionally runs
  `golangci-lint run --fix` to silently apply autofix-capable
  diagnostics before reporting anything remaining. `gochecks fix`
  implies `auto_fix` and so always does this.
- **Disable `modernize` in your `.golangci.yml`**: `go fix` (Go 1.26+)
  `go fix` (always on in gochecks) already runs the same modernizer
  suite. Leaving both enabled double-reports the same diagnostics.

## Verifying the hook is wired

```bash
gochecks doctor            # toolchain, tool health, hook wiring for this repo
gochecks status            # config and the last 20 hook invocations
tail -f ~/Library/Caches/gochecks/hook.log
```

Every invocation appends a line with duration, exit code, finding
count, and (for `hook`) the edited file. Edit a `.go` file through
Claude Code and an `args=[hook] file=...` line should appear.

To run the hook by hand, feed it an event:

```bash
printf '{"cwd":"%s","tool_input":{"file_path":"%s/main.go"}}' "$PWD" "$PWD" | gochecks hook
echo "exit=$?"
```
