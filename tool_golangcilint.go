package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command("golangci-lint", args...)
	cmd.Dir = pkgDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()
	return parseGolangciLint(stdout.String(), pkgDir), nil
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
		file := is.Pos.Filename
		if file == "" {
			continue
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(pkgDir, file)
		}
		file = filepath.Clean(file)
		class := "warning"
		if is.Severity == "error" {
			class = "error"
		}
		// Some linters are covered by our direct-probe path
		// (errcheck, ineffassign, staticcheck, govet). If users have
		// enabled gocheck's linters umbrella, the umbrella owns them;
		// skip the duplicate class overrides.
		fs = append(fs, Finding{
			File:  file,
			Line:  is.Pos.Line,
			Col:   is.Pos.Column,
			Class: class,
			Tool:  "golangci-lint/" + is.FromLinter,
			Msg:   is.Text,
		})
	}
	return fs
}
