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
	stdout, stderr := runTool(pkgDir, "staticcheck", "-f", "json", ".")
	return parseStaticcheck(stdout+stderr, pkgDir), nil
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
