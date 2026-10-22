# Claude Code integration

## Install per repo

`gochecks install` writes the hook into `<repo>/.claude/settings.json`,
not `~/.claude/settings.json`. Language-specific tooling belongs with
the repo it serves.

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

## Files are rewritten in place

`gochecks hook` and `check` run `gofmt -s -w` and `goimports -w` on the
changed files before anything else, so the hook can rewrite a file the
model just wrote. Claude Code refreshes its view on the next read or
edit. `check --no-apply` reports formatting as suggestions instead, for
CI.

## Skipped files

Dropped before any tool runs:

- anything not ending in `.go`, directories, and missing files
- any path with a `vendor/` segment
- files with a `// Code generated ... DO NOT EDIT.` line before the
  package clause
- patterns listed under `exclude` in `.gochecks.json`

## Optional tools

staticcheck, errcheck and ineffassign are used when on PATH and skipped
when not. golangci-lint runs only with `"linters": true`; it then owns
errcheck and ineffassign, and with `auto_fix` runs with `--fix`.
Disable its `modernize` linter, since `go fix` already covers it.
`gochecks doctor` lists what is installed and whether each tool still
works against the current Go toolchain; `doctor --fix` installs or
updates them.

## Checking the wiring

```sh
gochecks doctor
gochecks status
tail -f ~/Library/Caches/gochecks/hook.log
```

Every run appends a line with duration, exit code, finding count and,
for `hook`, the file. To drive the hook by hand:

```sh
printf '{"cwd":"%s","tool_input":{"file_path":"%s/main.go"}}' "$PWD" "$PWD" | gochecks hook
```
