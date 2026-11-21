# Claude Code integration

## Install per repo

`gochecks install` writes the hook into `<repo>/.claude/settings.json`,
not `~/.claude/settings.json`. Language-specific tooling belongs with
the repo it serves.

Claude Code reads `.claude/settings.json` only from the directory a
session starts in. Install at every place a Go session may start: each
module, and a `go.work` root even when it is not a repo. The hook
resolves each edited file to its module, so one install at a workspace
root covers edits in every module it lists.

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
    ],
    "SessionStart": [
      {
        "hooks": [
          { "type": "command", "command": "gochecks hook" }
        ]
      }
    ]
  }
}
```

The hook reads the PostToolUse event from stdin, takes
`tool_input.file_path`, and returns silently for anything that is not
a Go source file. Hooks inherit the parent environment, so `gochecks`
has to be on that PATH.

## What the model sees

Claude Code passes two things from a PostToolUse hook to the model:
stderr on exit 2, and a JSON object on stdout on exit 0. Plain stdout
goes to the debug log, and exit 1 shows the user a notice.

| Findings                    | Channel                                | Exit |
|-----------------------------|----------------------------------------|------|
| none                        | nothing                                | 0    |
| warnings / suggestions only | `hookSpecificOutput.additionalContext` | 0    |
| any `error`                 | text on stderr                         | 2    |
| hook misconfigured          | message on stderr, shown to the user   | 1    |

Both forms carry the same text, one line per finding with paths
relative to the session directory:

```
gochecks: 2 finding(s)
pkg/a.go:3:4 warning/staticcheck: this value of x is never used (SA4006)
pkg/a.go:9:2 suggestion/modernize: Loop can be simplified using slices.Contains (slicescontains; conflicts with another fix, apply by hand)
```

`check` and `fix` keep their own exit codes (0, 1, 2 by highest class)
for shell and CI use.

## Session start

`install` also wires `gochecks hook` as a SessionStart hook. On that
event the hook runs `deps` for the session directory and, only when
something is behind, adds one line of context:

```
gochecks: 2 dependencies behind: plinth v0.2.0 -> v0.4.0, htmx.min.js 2.0.4 -> 2.0.11 (gochecks deps --text)
```

Network failures and repos without a go.mod stay silent.

## Stop: the blocking check

`gochecks install --stop` also wires `gochecks hook` on Stop and
UserPromptSubmit, and adds `Bash(gochecks allow:*)` to
`permissions.deny`.

When the agent tries to end its turn, the hook checks every `.go` file
that differs from HEAD or is untracked and not ignored, in the repo at
the session directory or, at a `go.work` root that is not a repo, in
the repo of each module it uses. This sees edits made through Bash,
which PostToolUse never does. It runs `check --no-apply` on them, with
each file's `.gochecks.json` and its `exclude`.

- Errors or warnings: exit 2 with the findings on stderr. Claude Code
  hands them to the agent and the turn goes on.
- Suggestions only: exit 0. They come with the next prompt as context,
  once; context added at Stop would keep the turn going.
- git not on PATH: exit 1 with a note; the check cannot see changes.

The report tells the agent that touching a file means owning every
finding in it, and to ask the user when a finding looks wrong.

### Override

```sh
gochecks allow "<reason>"
gochecks allow --session <id> "<reason>"
```

`allow` releases the most recent block of a session started in the
current directory or below it, once, and only for the same blocking
set: a new or changed finding blocks again. A reason is required.
Grants and their use are logged to `~/Library/Caches/gochecks/stop/allow.log`
(the user cache dir elsewhere) and the run to `hook.log`.

The deny rule refuses `gochecks allow` from the agent's Bash tool. It
is not a security boundary: Claude Code matches the command text, so
another spelling gets past it. Run `allow` in your own terminal, or
type `! gochecks allow "<reason>"` in the session: Claude Code's docs
describe `!` commands as running without Claude, outside the
permission rules.

### A stuck agent

When the agent stops again with `stop_hook_active` set and the same
blocking set, it made no progress. The hook lets the turn end, marks
the findings unresolved, and adds one line to every following prompt
and to the next session started in the same directory:

```
gochecks: 3 blocking finding(s) from the last stop still stand in /path/repo. Fix them, or ask the user to run: gochecks allow "<reason>"
```

The line stops once a Stop check passes or `allow` is run. Claude Code
also ends a turn on its own after eight blocks in a row.

## Files are rewritten in place

`gochecks hook` and `check` run `gofmt -s -w` and `goimports -w` on the
changed files before anything else, so the hook can rewrite a file the
model just wrote. Claude Code refreshes its view on the next read or
edit. `check --no-apply` writes nothing, `auto_fix` included, and reports
formatting, modernize rewrites and lint fixes as findings instead, for
CI.

## Skipped files

Dropped before any tool runs:

- anything not ending in `.go`, directories, and missing files
- any path with a `vendor/` segment
- files with a `// Code generated ... DO NOT EDIT.` line before the
  package clause
- patterns listed under `exclude` in `.gochecks.json`

## Optional tools

gofumpt, staticcheck, errcheck, ineffassign and golangci-lint are used
when on PATH and skipped when not; `"linters": false` turns
golangci-lint off, and `"linters": true` makes a missing golangci-lint
a warning. gofumpt replaces gofmt when present. golangci-lint
takes the repo's `.golangci.yml` if there is one and the profile
embedded in gochecks otherwise; it then owns staticcheck, errcheck and
ineffassign, and with `auto_fix` runs with `--fix`. If you keep your own config,
leave its `modernize` linter off, since `go fix` already covers it.
`gochecks doctor` lists what is installed and whether each tool still
works against the current Go toolchain; `doctor --fix` installs or
updates them.

## Checking the wiring

```sh
gochecks doctor
gochecks status
tail -f ~/Library/Caches/gochecks/hook.log
```

`gochecks doctor --scan <dir>` walks `dir` and lists every directory
with a `go.mod` or `go.work` that has no `.claude/settings.json`, no
`gochecks hook` on PostToolUse, or still runs `gocheck`, the old name,
in its settings or Makefile. It skips `testdata`, `vendor`,
`node_modules`, hidden directories and directories git ignores, and
exits 1 when it lists anything. `--require-stop` also lists a launch
point whose settings lack the Stop hook; use it once every repo under
`dir` is meant to have one.

Every run appends a line with duration, exit code, finding count and,
for `hook`, the file. To drive the hook by hand:

```sh
printf '{"cwd":"%s","tool_input":{"file_path":"%s/main.go"}}' "$PWD" "$PWD" | gochecks hook
```
