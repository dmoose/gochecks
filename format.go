package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var errGoimportsMissing = errors.New("goimports binary not found on PATH")

// runGofmt formats with gofumpt when it is on PATH, otherwise gofmt -s.
// gofumpt applies gofmt's rules plus a stricter set of its own.
func runGofmt(files []string, apply bool) ([]Finding, error) {
	tool, args := "gofmt", []string{"-s"}
	if _, err := exec.LookPath("gofumpt"); err == nil {
		tool, args = "gofumpt", nil
	}
	if apply {
		args = append(args, "-w")
	} else {
		args = append(args, "-l")
	}
	out, parseErrs, err := formatCmd(tool, append(args, files...))
	if err != nil || apply {
		return parseErrs, err
	}
	return append(parseErrs, linesToFindings(out, tool, "needs "+tool)...), nil
}

// runGoimports runs goimports per module so -local can name the module
// path and the repo's own imports group separately from third-party.
func runGoimports(files []string, apply bool) ([]Finding, error) {
	if _, err := exec.LookPath("goimports"); err != nil {
		return nil, errGoimportsMissing
	}
	mode := "-l"
	if apply {
		mode = "-w"
	}
	var fs []Finding
	for mod, group := range groupByModule(files) {
		args := []string{mode}
		if mod != "" {
			args = append(args, "-local", mod)
		}
		out, parseErrs, err := formatCmd("goimports", append(args, group...))
		if err != nil {
			return nil, err
		}
		fs = append(fs, parseErrs...)
		if !apply {
			fs = append(fs, linesToFindings(out, "goimports", "needs goimports")...)
		}
	}
	return fs, nil
}

// formatCmd runs a formatter. A file that does not parse is an error
// finding at its position, not a failed run: the other files are still
// formatted, and a syntax error must block like any other error.
func formatCmd(tool string, args []string) (string, []Finding, error) {
	cmd := exec.Command(tool, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if fs := textFindings(stderr.String(), "", tool, "error"); len(fs) > 0 {
			return stdout.String(), fs, nil
		}
		return "", nil, fmt.Errorf("%s: %w: %s", tool, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil, nil
}

// groupByModule buckets files by the module path of the nearest go.mod.
func groupByModule(files []string) map[string][]string {
	out := map[string][]string{}
	for _, f := range files {
		mod := modulePath(filepath.Dir(f))
		out[mod] = append(out[mod], f)
	}
	return out
}

var (
	moduleMu    sync.Mutex
	moduleCache = map[string]string{}
)

// modulePath returns the module path declared by the nearest go.mod at
// or above dir, or "" when there is none.
func modulePath(dir string) string {
	moduleMu.Lock()
	defer moduleMu.Unlock()
	if m, ok := moduleCache[dir]; ok {
		return m
	}
	mod := ""
	for d := dir; ; d = filepath.Dir(d) {
		if m, ok := readModulePath(filepath.Join(d, "go.mod")); ok {
			mod = m
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	moduleCache[dir] = mod
	return mod
}

func readModulePath(gomod string) (string, bool) {
	f, err := os.Open(gomod)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), true
		}
	}
	return "", true
}

func linesToFindings(out, tool, msg string) []Finding {
	var fs []Finding
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fs = append(fs, Finding{File: line, Class: "suggestion", Tool: tool, Msg: msg})
	}
	return fs
}
