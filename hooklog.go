package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cacheDir returns the directory used for gocheck's own artifacts
// (currently just hook.log). The name is historical — an earlier
// version kept a per-tool finding cache here; that's been removed in
// favor of Go's own build cache, which handles transitive
// invalidation correctly across packages.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "gocheck")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// invocationFindings is set by subcommands before they return, so the
// hook log entry can record how many findings were emitted.
var invocationFindings int

// logInvocation appends a single line to <cacheDir>/hook.log summarizing
// the run. Called unconditionally from main so that even failed or
// silent runs leave a trace — this solves the "did the hook actually
// fire?" observability gap.
//
// Failures here are swallowed: logging must never affect the exit path.
func logInvocation(args []string, code int, dur time.Duration) {
	dir, err := cacheDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "hook.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	ts := time.Now().UTC().Format(time.RFC3339)
	_, _ = fmt.Fprintf(f, "%s dur=%s exit=%d findings=%d args=[%s]\n",
		ts, dur.Round(time.Millisecond), code, invocationFindings, strings.Join(args, " "))
}
