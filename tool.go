package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// analyzer is one per-package pass run after a successful build.
type analyzer struct {
	name string
	// enabled decides whether the pass runs for this invocation.
	enabled func(cfg Config, p toolProbes) bool
	run     func(pkgDir string, cfg Config) ([]Finding, error)
	// unscoped passes keep findings outside the changed files: a test
	// failure points at the test file, which is rarely the edit.
	unscoped bool
}

var analyzers = []analyzer{
	{name: "vet", enabled: always, run: ignoreCfg(runVet)},
	{name: "staticcheck", enabled: func(_ Config, p toolProbes) bool { return p.staticcheck }, run: ignoreCfg(runStaticcheck)},
	{name: "errcheck", enabled: func(_ Config, p toolProbes) bool { return p.errcheck }, run: ignoreCfg(runErrcheck)},
	{name: "ineffassign", enabled: func(_ Config, p toolProbes) bool { return p.ineffassign }, run: ignoreCfg(runIneffassign)},
	{name: "golangci-lint", enabled: func(_ Config, p toolProbes) bool { return p.golangci }, run: func(d string, c Config) ([]Finding, error) { return runGolangciLint(d, c.AutoFix) }},
	{name: "test", enabled: func(c Config, _ toolProbes) bool { return c.Test }, run: func(d string, c Config) ([]Finding, error) { return runGoTest(d, c.testTimeout()) }, unscoped: true},
}

func always(Config, toolProbes) bool { return true }

func ignoreCfg(f func(string) ([]Finding, error)) func(string, Config) ([]Finding, error) {
	return func(d string, _ Config) ([]Finding, error) { return f(d) }
}

// runTool runs name in dir and returns both streams. Exit status is
// deliberately ignored: every tool here exits non-zero on findings.
func runTool(dir, name string, args ...string) (stdout, stderr string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	_ = cmd.Run()
	return out.String(), errb.String()
}

// absIn anchors a tool-reported path to the package directory.
func absIn(pkgDir, file string) string {
	if !filepath.IsAbs(file) {
		file = filepath.Join(pkgDir, file)
	}
	return filepath.Clean(file)
}

// posLineRE matches the common "file:line:col: message" diagnostic line.
var posLineRE = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(.+)$`)

// textFindings parses file:line:col lines into findings of one class.
func textFindings(out, pkgDir, tool, class string) []Finding {
	var fs []Finding
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := posLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ln, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		fs = append(fs, Finding{File: absIn(pkgDir, m[1]), Line: ln, Col: col, Class: class, Tool: tool, Msg: m[4]})
	}
	return fs
}

// parsePosn splits "file:line:col" (or "file:line") as printed by the
// go/analysis JSON output.
func parsePosn(s string) (file string, line, col int) {
	file = s
	if i := strings.LastIndex(file, ":"); i > 0 {
		if n, err := strconv.Atoi(file[i+1:]); err == nil {
			col, file = n, file[:i]
		}
	}
	if i := strings.LastIndex(file, ":"); i > 0 {
		if n, err := strconv.Atoi(file[i+1:]); err == nil {
			line, file = n, file[:i]
		}
	}
	if line == 0 {
		// Only one number present: it was the line.
		line, col = col, 0
	}
	return file, line, col
}

// analysisDiag is a go/analysis diagnostic as emitted by `go vet -json`
// and `go fix -json`: {pkg: {analyzer: [diag...]}}.
type analysisDiag struct {
	Posn    string `json:"posn"`
	Message string `json:"message"`
}

func parseAnalysisJSON(out string) map[string]map[string][]analysisDiag {
	out = strings.TrimSpace(out)
	if !strings.HasPrefix(out, "{") {
		return nil
	}
	var m map[string]map[string][]analysisDiag
	if json.Unmarshal([]byte(out), &m) != nil {
		return nil
	}
	return m
}

// analysisFindings flattens the JSON tree into findings in a stable
// order. msg receives the analyzer name and message.
func analysisFindings(tree map[string]map[string][]analysisDiag, pkgDir, tool, class string, msg func(analyzer, message string) string) []Finding {
	var fs []Finding
	for _, byAnalyzer := range tree {
		for name, diags := range byAnalyzer {
			for _, d := range diags {
				file, line, col := parsePosn(d.Posn)
				fs = append(fs, Finding{File: absIn(pkgDir, file), Line: line, Col: col, Class: class, Tool: tool, Msg: msg(name, d.Message)})
			}
		}
	}
	sortFindings(fs)
	return fs
}

func sortFindings(fs []Finding) {
	slices.SortFunc(fs, func(a, b Finding) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Col - b.Col
	})
}

func filterScope(findings []Finding, scope map[string]bool) []Finding {
	var out []Finding
	for _, f := range findings {
		if scope[f.File] {
			out = append(out, f)
		}
	}
	return out
}
