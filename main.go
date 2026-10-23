package main

import (
	_ "embed"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

// lintProfile is the golangci-lint configuration used for repos that
// have none of their own. The same file lints this repo.
//
//go:embed .golangci.yml
var lintProfile string

// version is set by release builds with -X main.version; go install
// builds report the module version from build info.
var version = "dev"

func versionString() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return version
}

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
	case "deps":
		return runDeps(args[1:])
	case "lint-config":
		fmt.Print(lintProfile)
		return 0
	case "version", "--version", "-v":
		fmt.Println("gochecks", versionString())
		return 0
	case "help", "--help", "-h":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "gochecks: unknown command %q\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w *os.File) {
	_, _ = fmt.Fprintln(w, `gochecks: Go toolchain checks for Claude Code hooks

Usage:
  gochecks hook
  gochecks check  [--changed "<file>..."] [--no-apply] [--text] [file...]
  gochecks fix    [--changed "<file>..."] [--text] [file...]
  gochecks show   [--context N] <file:line>
  gochecks doctor [--fix] [--offline]
  gochecks status
  gochecks deps   [--fail] [--text]
  gochecks install [--dir .] [--upgrade] [--force] [--no-hook] [--no-skill] [--no-doc] [--config] [--lint-config]
  gochecks lint-config
  gochecks version

hook         Claude Code PostToolUse entry point. Reads the event from stdin
             and checks the edited Go file. Errors go to stderr with exit 2;
             warnings and suggestions return as additionalContext.
check        Formats with gofumpt (or gofmt) and goimports, then runs go
             build, go vet and the installed analyzers (staticcheck,
             errcheck, ineffassign, golangci-lint) on the changed packages.
             Findings outside the changed files are dropped. Exit 0, 1 or 2
             by highest class. --no-apply reports formatting instead.
fix          check, plus go fix modernize on the changed packages first.
show         Source context and vet findings at file:line.
doctor       Go toolchain and each tool: installed, built against the
             current toolchain, latest release; the repo's hook wiring.
             --fix installs or updates.
status       Config in effect and the hook log tail.
deps         Direct Go modules and copied assets (the assets map in
             .gochecks.json, file -> npm package) with a newer release,
             own modules first. --fail exits 1 when anything is behind.
install      Wire a repo: PostToolUse hook in .claude/settings.json, the
             go-current skill, .claude/gochecks.md. --upgrade refreshes
             those; --config adds .gochecks.json; --lint-config adds the
             profile as .golangci.yml, never overwriting one.
lint-config  Print the golangci-lint profile used for repos without one.

Output is JSON lines by default; --text for one finding per line.
Positional file args are equivalent to --changed entries.

Config: .gochecks.json at the repo root (test, test_timeout, auto_fix,
linters, exclude). See HOOKS.md for the Claude Code integration.`)
}
