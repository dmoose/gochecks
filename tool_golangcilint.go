package main

import (
	"encoding/json"
	"os/exec"
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

// runGolangciLint runs golangci-lint, with --fix when autoFix is set;
// the report then holds only what was not fixed.
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
