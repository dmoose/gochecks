package main

import (
	"fmt"
	"os"
	"time"
)

const version = "0.7.0-dev"

func main() {
	start := time.Now()
	code := dispatch(os.Args[1:])
	logInvocation(os.Args[1:], code, time.Since(start))
	os.Exit(code)
}

func dispatch(args []string) int {
	if len(args) < 1 {
		usage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:])
	case "fix":
		return runFix(args[1:])
	case "show":
		return runShow(args[1:])
	case "status":
		return runStatus(args[1:])
	case "install":
		return runInstall(args[1:])
	case "hook":
		return runHook(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "version", "--version", "-v":
		fmt.Println("gocheck", version)
		return 0
	case "help", "--help", "-h":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "gocheck: unknown command %q\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w *os.File) {
	_, _ = fmt.Fprintln(w, `gocheck — context-efficient Go toolchain dispatcher

Usage:
  gocheck check [flags] [--changed "<file>..."] [file...]
  gocheck fix   [flags] [--changed "<file>..."] [file...]
  gocheck show    [--context N] <file:line>
  gocheck hook
  gocheck status
  gocheck doctor  [--fix] [--offline]
  gocheck install [--dir .] [--force] [--upgrade] [--no-hook] [--no-skill] [--no-doc] [--config]
  gocheck version
  gocheck help

Flags for 'check':
  --changed <list>   whitespace- or comma-separated files the agent touched
  --no-apply         don't auto-fix; report formatting as suggestions
  --text             human-readable output (default: JSON lines)

Flags for 'fix':
  --changed <list>   whitespace- or comma-separated files the agent touched
  --text             human-readable output (default: JSON lines)

'check' silently applies gofmt + goimports, then runs analyzers.
'fix'   additionally runs 'go fix' modernize on the changed packages
        before re-checking.

'doctor' checks the Go toolchain and every tool gocheck drives: present,
        built against the current toolchain, current with its latest
        release, and the repo's hook wiring. --fix runs the installs.
'hook'  reads a Claude Code PostToolUse event from stdin and checks the
        edited file. Errors go to stderr with exit 2; warnings and
        suggestions return as additionalContext. Wired by 'install'.

Positional file args are equivalent to --changed entries.

See DESIGN.md for scope and roadmap.`)
}
