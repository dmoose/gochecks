package main

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

const modernizeMaxIterations = 10

// runModernize applies go fix to the files in scope within pkgDir and
// returns those it changed. go fix -diff prints the merged result of a
// pass without writing; only the hunks of files in scope are applied,
// so the package's other files are left alone. A pass can leave fixes
// that conflicted with applied ones, so passes repeat until the diff
// for the files in scope is empty or stops changing.
func runModernize(pkgDir string, scope map[string]bool) ([]string, error) {
	changed := map[string]bool{}
	var prev string
	for range modernizeMaxIterations {
		diffs, raw, err := modernizeDiff(pkgDir, scope)
		if err != nil {
			return nil, err
		}
		if len(diffs) == 0 || raw == prev {
			return slices.Sorted(maps.Keys(changed)), nil
		}
		prev = raw
		for _, d := range diffs {
			if err := d.apply(); err != nil {
				return nil, fmt.Errorf("go fix: %s: %w", d.path, err)
			}
			changed[d.path] = true
		}
	}
	return nil, fmt.Errorf("go fix: exceeded %d iterations in %s", modernizeMaxIterations, pkgDir)
}

// modernizeDiff runs go fix -diff and returns the file diffs in scope,
// with their text for comparing passes.
func modernizeDiff(pkgDir string, scope map[string]bool) ([]fileDiff, string, error) {
	cmd := exec.Command("go", "fix", "-diff", ".")
	cmd.Dir = pkgDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	// -diff exits 1 when the diff is not empty.
	if err != nil && !strings.HasPrefix(stdout.String(), "--- ") {
		return nil, "", fmt.Errorf("go fix: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	all, err := parseUnifiedDiff(stdout.String())
	if err != nil {
		return nil, "", fmt.Errorf("go fix -diff: %w", err)
	}
	var diffs []fileDiff
	var raw strings.Builder
	for _, d := range all {
		if scope[d.path] {
			diffs = append(diffs, d)
			raw.WriteString(d.text)
		}
	}
	return diffs, raw.String(), nil
}

// modernizePending reports the rewrites go fix would make, without
// writing anything.
func modernizePending(pkgDir string) ([]Finding, error) {
	stdout, _ := runTool(pkgDir, "go", "fix", "-json", ".")
	return analysisFindings(parseAnalysisJSON(stdout), pkgDir, "modernize", "suggestion",
		func(analyzer, msg string) string {
			return msg + " (" + analyzer + "; gochecks fix applies it)"
		}), nil
}

// modernizeConflicts lists the fixes go fix left unapplied, each at
// its own position.
func modernizeConflicts(pkgDir string) []Finding {
	stdout, _ := runTool(pkgDir, "go", "fix", "-json", ".")
	return parseModernize(stdout, pkgDir)
}

func parseModernize(out, pkgDir string) []Finding {
	return analysisFindings(parseAnalysisJSON(out), pkgDir, "modernize", "suggestion",
		func(analyzer, msg string) string {
			return msg + " (" + analyzer + "; conflicts with another fix, apply by hand)"
		})
}

// fileDiff is one file's part of a unified diff as go fix -diff
// prints it: "--- path (old)", "+++ path (new)", then hunks.
type fileDiff struct {
	path  string
	hunks []hunk
	text  string
}

type hunk struct {
	oldStart, oldCount int
	lines              []string // with their ' ', '-', '+' or '\' prefix
}

func parseUnifiedDiff(out string) ([]fileDiff, error) {
	var diffs []fileDiff
	var cur *fileDiff
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "):
			diffs = append(diffs, fileDiff{path: strings.TrimSuffix(strings.TrimPrefix(line, "--- "), " (old)")})
			cur = &diffs[len(diffs)-1]
		case cur == nil || line == "":
			continue
		case strings.HasPrefix(line, "+++ "):
		case strings.HasPrefix(line, "@@ "):
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			cur.hunks = append(cur.hunks, h)
		case len(cur.hunks) > 0 && strings.ContainsAny(line[:1], " -+\\"):
			h := &cur.hunks[len(cur.hunks)-1]
			h.lines = append(h.lines, line)
		default:
			return nil, fmt.Errorf("unexpected line %q", line)
		}
		if cur != nil {
			cur.text += line + "\n"
		}
	}
	return diffs, nil
}

// parseHunkHeader reads "@@ -l,s +l,s @@"; a missing count is 1.
func parseHunkHeader(line string) (hunk, error) {
	f := strings.Fields(line)
	if len(f) < 3 || !strings.HasPrefix(f[1], "-") {
		return hunk{}, fmt.Errorf("bad hunk header %q", line)
	}
	start, count, hasCount := strings.Cut(f[1][1:], ",")
	s, err := strconv.Atoi(start)
	if err != nil {
		return hunk{}, fmt.Errorf("bad hunk header %q", line)
	}
	c := 1
	if hasCount {
		if c, err = strconv.Atoi(count); err != nil {
			return hunk{}, fmt.Errorf("bad hunk header %q", line)
		}
	}
	return hunk{oldStart: s, oldCount: c}, nil
}

var errDiffMismatch = errors.New("diff does not match the file")

// apply rewrites the file with its hunks, checking that every context
// and removed line matches.
func (d fileDiff) apply() error {
	fi, err := os.Stat(d.path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		return err
	}
	old := strings.SplitAfter(string(data), "\n")
	if old[len(old)-1] == "" {
		old = old[:len(old)-1]
	}
	var out strings.Builder
	i := 0
	for _, h := range d.hunks {
		start := h.oldStart - 1
		if h.oldCount == 0 {
			start = h.oldStart // pure insertion after line oldStart
		}
		if start < i || start > len(old) {
			return errDiffMismatch
		}
		for ; i < start; i++ {
			out.WriteString(old[i])
		}
		lastAdded := false
		for _, l := range h.lines {
			op, text := l[0], l[1:]
			switch op {
			case ' ', '-':
				if i >= len(old) || strings.TrimSuffix(old[i], "\n") != text {
					return errDiffMismatch
				}
				if op == ' ' {
					out.WriteString(old[i])
				}
				i++
				lastAdded = false
			case '+':
				out.WriteString(text + "\n")
				lastAdded = true
			case '\\':
				// "\ No newline at end of file" after an added line.
				if lastAdded {
					s := strings.TrimSuffix(out.String(), "\n")
					out.Reset()
					out.WriteString(s)
				}
			}
		}
	}
	for ; i < len(old); i++ {
		out.WriteString(old[i])
	}
	return os.WriteFile(d.path, []byte(out.String()), fi.Mode().Perm())
}
