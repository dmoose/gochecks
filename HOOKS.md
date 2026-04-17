# gocheck — Claude Code hook integration

How to wire `gocheck` into Claude Code as a `PostToolUse` hook so that
every agent-driven edit of a `.go` file is mechanically sanitized
before the next turn.

## Install at project scope, not user scope

Install the hook at `<repo>/.claude/settings.json` — **never** at
`~/.claude/settings.json`. Language-gated tooling belongs with the
repo it serves. User scope would fire gocheck inside TypeScript,
markdown, Xcode, etc. repos and either waste cycles or error out.

## Canonical `settings.json`

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write|MultiEdit",
        "hooks": [
          {
            "type": "command",
            "command": "for p in $CLAUDE_FILE_PATHS; do case \"$p\" in *.go) exec /absolute/path/to/gocheck check --changed \"$CLAUDE_FILE_PATHS\" ;; esac; done"
          }
        ]
      }
    ]
  }
}
```

- Matches Claude Code's three edit tools (`Edit`, `Write`,
  `MultiEdit`).
- The `case` guard on `*.go` short-circuits non-Go edits without
  spawning gocheck at all.
- `$CLAUDE_FILE_PATHS` is a whitespace-separated list of the files
  the tool call touched. gocheck's `--changed` accepts whitespace-
  or comma-separated lists.
- Use an absolute path to the `gocheck` binary. `$PATH` inside hook
  shells is minimal.

For a live example see [`.claude/settings.json`](./.claude/settings.json)
in this repo.

## Exit-code semantics

| Exit | Meaning                            | Hook treatment   |
|------|------------------------------------|------------------|
| 0    | Clean, or only `suggestion`-class  | Silent pass      |
| 1    | At least one `warning`             | Nudge            |
| 2    | At least one `error` (vet, etc.)   | Block next turn  |

Claude Code's hook layer surfaces non-zero exits to the agent as a
system message. Exit 2 is reserved for semantically-wrong code;
exit 1 is for style/lint warnings that should be noticed but not
blocking.

## Silent mid-turn file rewrites

`gocheck check` auto-applies `gofmt -s -w` and `goimports -w` to the
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

`gocheck` filters its own input before running any tool. The
following are skipped unconditionally in `--changed`:

- Files not ending in `.go`
- Directories
- Missing files (tool may have deleted them)
- Any path containing a `vendor/` segment
- Files whose header line matches `// Code generated ... DO NOT EDIT.`
  anywhere before the `package` clause

The agent's hook doesn't need to pre-filter `$CLAUDE_FILE_PATHS`.

## Optional tools

`gocheck` probes for these at runtime and uses them when present;
absent ones are silently skipped:

- `staticcheck` — extended static analysis, class `warning`.
- `errcheck` — unchecked error returns, class `warning`.
- `ineffassign` — wasted assignments, class `warning`.
- `golangci-lint` — umbrella of 100+ linters. Opt-in only: enable
  via `"linters": true` in `.gocheck.json`. Off by default because
  of its 0.5-2s cold startup.

### Interaction with `golangci-lint`

When `linters: true` and `golangci-lint` is on PATH:

- gocheck defers `errcheck` and `ineffassign` coverage to the
  umbrella (the umbrella runs both, so gocheck does not to avoid
  double-reporting).
- With `auto_fix: true`, gocheck additionally runs
  `golangci-lint run --fix` to silently apply autofix-capable
  diagnostics before reporting anything remaining. `gocheck fix`
  implies `auto_fix` and so always does this.
- **Disable `modernize` in your `.golangci.yml`**: Go 1.26's
  `go fix` (always on in gocheck) already runs the same modernizer
  suite. Leaving both enabled double-reports the same diagnostics.

## Verifying the hook is wired

From the repo root:

```bash
# Touch a .go file and confirm gocheck runs against it.
./gocheck check --changed some_file.go
echo "exit=$?"
```

If you want to observe hook firings, add a trace line:

```json
"command": "echo \"[gocheck-hook] $CLAUDE_FILE_PATHS\" >> /tmp/gocheck-hook.log; for p in $CLAUDE_FILE_PATHS; do case \"$p\" in *.go) exec /absolute/path/to/gocheck check --changed \"$CLAUDE_FILE_PATHS\" ;; esac; done"
```

Then `tail -f /tmp/gocheck-hook.log` while the agent works.
