package main

import (
	"encoding/json"
	"os/exec"
)

// golangci-lint v2 JSON output shape — we only read the subset we need.
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

// runGolangciLint runs the umbrella linter. When autoFix is true,
// `--fix` silently applies any autofix-capable diagnostics in place;
// the JSON report then contains only the issues that weren't fixed.
// We never treat a nonzero exit as an error — golangci-lint exits
// nonzero whenever any issues remain, which is the expected case.
func runGolangciLint(pkgDir string, autoFix bool) ([]Finding, error) {
	args := []string{"run", "--output.json.path=stdout"}
	if autoFix {
		args = append(args, "--fix")
	}
	args = append(args, "./...")
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
	var rep glReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		return nil
	}
	fs := make([]Finding, 0, len(rep.Issues))
	for _, is := range rep.Issues {
		if is.FromLinter == "typecheck" && toolBroken(is.Text) {
			return brokenFinding("golangci-lint", pkgDir)
		}
		file := is.Pos.Filename
		if file == "" {
			continue
		}
		class := "warning"
		if is.Severity == "error" {
			class = "error"
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
