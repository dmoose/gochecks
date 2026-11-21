package main

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// staticcheckDiag is one line of `staticcheck -f json`.
type staticcheckDiag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Location struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"location"`
	Message string `json:"message"`
}

func staticcheckAvailable() bool {
	_, err := exec.LookPath("staticcheck")
	return err == nil
}

func runStaticcheck(pkgDir string) ([]Finding, error) {
	r := runTool(pkgDir, "staticcheck", "-f", "json", ".")
	fs := parseStaticcheck(r.stdout+r.stderr, pkgDir)
	if len(fs) == 0 {
		return failedRun(r, pkgDir, "staticcheck", "warning", staticcheckErrors(r.stdout)+r.stderr), nil
	}
	return fs, nil
}

// staticcheckErrors returns the messages of diagnostics that name no
// file, which is how staticcheck reports a package it could not load.
func staticcheckErrors(out string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(out, "\n") {
		var d staticcheckDiag
		if json.Unmarshal([]byte(line), &d) == nil && d.Location.File == "" {
			b.WriteString(d.Message + "\n")
		}
	}
	return b.String()
}

func parseStaticcheck(out, pkgDir string) []Finding {
	if toolBroken(out) {
		return brokenFinding("staticcheck", pkgDir)
	}
	var fs []Finding
	for line := range strings.SplitSeq(out, "\n") {
		var d staticcheckDiag
		if json.Unmarshal([]byte(line), &d) != nil || d.Severity == "ignored" || d.Location.File == "" {
			continue
		}
		fs = append(fs, Finding{
			File:  absIn(pkgDir, d.Location.File),
			Line:  d.Location.Line,
			Col:   d.Location.Column,
			Class: "warning",
			Tool:  "staticcheck",
			Msg:   d.Message + " (" + d.Code + ")",
		})
	}
	return fs
}
