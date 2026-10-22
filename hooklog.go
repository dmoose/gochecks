package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cacheDir returns the per-user directory holding hook.log.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "gochecks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// invocationFindings is the finding count recorded in the log line.
var invocationFindings int

// invocationNote is extra detail for the log line; hook records the
// edited file here since it arrives on stdin rather than in args.
var invocationNote string

// logInvocation appends one line per run to hook.log. Logging failures
// are ignored so they cannot change the exit code.
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
	line := fmt.Sprintf("%s dur=%s exit=%d findings=%d args=[%s]",
		ts, dur.Round(time.Millisecond), code, invocationFindings, strings.Join(args, " "))
	if invocationNote != "" {
		line += " file=" + invocationNote
	}
	_, _ = fmt.Fprintln(f, line)
}
