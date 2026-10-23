package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// glReport is the part of the golangci-lint v2 JSON report read here.
type glReport struct {
	Issues []struct {
		FromLinter string `json:"FromLinter"`
		Text       string `json:"Text"`
		Severity   string `json:"Severity"`
		Pos        struct {
			Filename string `json:"Filename"`
			Line     int    `json:"Line"`
			Column   int    `json:"Column"`
		} `json:"Pos"`
	} `json:"Issues"`
}

func golangciLintAvailable() bool {
	_, err := exec.LookPath("golangci-lint")
	return err == nil
}

// suggestionLinters report idiom rather than bugs; their findings are
// suggestions. Everything else from golangci-lint is a warning.
var suggestionLinters = map[string]bool{
	"copyloopvar": true, "errname": true, "exptostd": true, "fatcontext": true,
	"iface": true, "intrange": true, "mirror": true, "misspell": true,
	"modernize": true, "nilnil": true, "perfsprint": true, "revive": true,
	"sloglint": true, "testifylint": true, "unconvert": true,
	"usestdlibvars": true, "usetesting": true,
}

// formatterLinters are golangci-lint's formatters. gochecks runs gofumpt
// and goimports itself, so their findings here would be duplicates.
var formatterLinters = map[string]bool{"gofmt": true, "gofumpt": true, "goimports": true, "gci": true, "golines": true}

var lintConfigNames = []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"}

// findLintConfig returns the repo's golangci-lint config, searching from
// dir up to the module root, or "" when the repo has none.
func findLintConfig(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		for _, name := range lintConfigNames {
			if _, err := os.Stat(filepath.Join(d, name)); err == nil {
				return filepath.Join(d, name)
			}
		}
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil || filepath.Dir(d) == d {
			return ""
		}
	}
}

// profilePath writes the embedded profile under the cache dir once per
// run and returns its path.
func profilePath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "golangci.yml")
	if data, err := os.ReadFile(path); err == nil && string(data) == lintProfile {
		return path, nil
	}
	return path, os.WriteFile(path, []byte(lintProfile), 0o644)
}

// runGolangciLint runs golangci-lint with the repo's config or the
// embedded profile, with --fix when autoFix is set; the report then
// holds only what was not fixed.
func runGolangciLint(pkgDir string, autoFix bool) ([]Finding, error) {
	// Packages are checked concurrently; without --allow-parallel-runners
	// a second golangci-lint waits on the first's lock or gives up.
	args := []string{"run", "--output.json.path=stdout", "--show-stats=false", "--path-mode=abs", "--allow-parallel-runners"}
	if findLintConfig(pkgDir) == "" {
		cfg, err := profilePath()
		if err != nil {
			return nil, err
		}
		args = append(args, "-c", cfg)
	}
	if autoFix {
		args = append(args, "--fix")
	}
	args = append(args, ".")
	stdout, stderr := runTool(pkgDir, "golangci-lint", args...)
	// A toolchain mismatch aborts before any JSON is written.
	if toolBroken(stderr) {
		return brokenFinding("golangci-lint", pkgDir), nil
	}
	return parseGolangciLint(stdout, pkgDir), nil
}

func parseGolangciLint(out, pkgDir string) []Finding {
	if out == "" {
		return nil
	}
	// The report is the first JSON value; anything after it is text.
	var rep glReport
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&rep); err != nil {
		return nil
	}
	fs := make([]Finding, 0, len(rep.Issues))
	for _, is := range rep.Issues {
		if is.FromLinter == "typecheck" && toolBroken(is.Text) {
			return brokenFinding("golangci-lint", pkgDir)
		}
		file := is.Pos.Filename
		if file == "" || formatterLinters[is.FromLinter] {
			continue
		}
		class := "warning"
		switch {
		case is.Severity == "error":
			class = "error"
		case suggestionLinters[is.FromLinter]:
			class = "suggestion"
		}
		fs = append(fs, Finding{
			File:  absIn(pkgDir, file),
			Line:  is.Pos.Line,
			Col:   is.Pos.Column,
			Class: class,
			Tool:  "golangci-lint/" + is.FromLinter,
			Msg:   is.Text,
		})
	}
	return fs
}
