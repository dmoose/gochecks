package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// launchProblem is one wiring gap at a Go launch point.
type launchProblem struct {
	dir string // the launch point
	msg string
}

// scanLaunchPoints walks root for Go launch points, directories that
// hold a go.mod or go.work, and returns them and every wiring gap
// found. Claude Code reads .claude/settings.json only from the
// directory a session starts in, so each launch point needs its own.
func scanLaunchPoints(root string) (points []string, problems []launchProblem, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is skipped, not fatal.
			if d != nil && d.IsDir() && path != root {
				return filepath.SkipDir
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipScanDir(d.Name()) {
			return filepath.SkipDir
		}
		if !isLaunchPoint(path) {
			return nil
		}
		// Ignored directories are generated output, where no session
		// starts. Neither is anything below them.
		if path != root && gitIgnored(path) {
			return filepath.SkipDir
		}
		points = append(points, path)
		for _, msg := range launchPointProblems(path) {
			problems = append(problems, launchProblem{dir: path, msg: msg})
		}
		return nil
	})
	return points, problems, err
}

// skipScanDir names directories that hold no launch point of their
// own: the go tool ignores testdata, vendor, and names starting with
// "." or "_". Directories git ignores are skipped too; see gitIgnored.
func skipScanDir(name string) bool {
	switch name {
	case "testdata", "vendor", "node_modules":
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func isLaunchPoint(dir string) bool {
	for _, name := range []string{"go.mod", "go.work"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// gitIgnored reports whether the repo enclosing dir ignores it. Outside
// a repo nothing is ignored.
func gitIgnored(dir string) bool {
	cmd := exec.Command("git", "-C", filepath.Dir(dir), "check-ignore", "-q", filepath.Base(dir)) //nolint:gosec // fixed git subcommand; dir comes from the walk
	return cmd.Run() == nil
}

// oldNameRE matches the pre-rename command name as a word.
var oldNameRE = regexp.MustCompile(`\bgocheck\b`)

// launchPointProblems lists the wiring gaps at one launch point.
func launchPointProblems(dir string) []string {
	var out []string
	settings := filepath.Join(".claude", "settings.json")
	data, err := os.ReadFile(filepath.Join(dir, settings))
	switch {
	case err != nil:
		out = append(out, "no "+settings+" (run gochecks install)")
	case !hasHookEntry(data, "PostToolUse"):
		out = append(out, "no gochecks hook in "+settings+" (run gochecks install)")
	}
	if err == nil && oldNameRE.Match(data) {
		out = append(out, settings+" runs gocheck, the old name (run gochecks install --upgrade)")
	}
	if makefileCallsOldName(filepath.Join(dir, "Makefile")) {
		out = append(out, "Makefile runs gocheck, the old name; use gochecks")
	}
	return out
}

// hasHookEntry reports whether settings wire `gochecks hook` for event.
func hasHookEntry(settings []byte, event string) bool {
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(settings, &s) != nil {
		return false
	}
	for _, entry := range s.Hooks[event] {
		for _, h := range entry.Hooks {
			if strings.Contains(h.Command, hookCommand) {
				return true
			}
		}
	}
	return false
}

// makefileCallsOldName reports a recipe or variable that runs gocheck.
func makefileCallsOldName(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if code, _, _ := strings.Cut(line, "#"); oldNameRE.MatchString(code) {
			return true
		}
	}
	return false
}

// runScan prints each gap under root, relative to it, and returns 1
// when there is any.
func runScan(root string, out io.Writer) int {
	abs, err := filepath.Abs(root)
	if err != nil {
		_, _ = fmt.Fprintln(out, "gochecks doctor --scan:", err)
		return 2
	}
	points, problems, err := scanLaunchPoints(abs)
	if err != nil {
		_, _ = fmt.Fprintln(out, "gochecks doctor --scan:", err)
		return 2
	}
	for _, p := range problems {
		rel, err := filepath.Rel(abs, p.dir)
		if err != nil {
			rel = p.dir
		}
		_, _ = fmt.Fprintf(out, "%s/: %s\n", rel, p.msg)
	}
	_, _ = fmt.Fprintf(out, "%d launch point(s) under %s, %d problem(s)\n", len(points), abs, len(problems))
	if len(problems) > 0 {
		return 1
	}
	return 0
}
