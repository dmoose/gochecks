package main

import (
	"os/exec"
	"strings"
)

func errcheckAvailable() bool {
	_, err := exec.LookPath("errcheck")
	return err == nil
}

// runErrcheck reports unchecked errors; errcheck prints the offending
// source line after the position.
func runErrcheck(pkgDir string) ([]Finding, error) {
	r := runTool(pkgDir, "errcheck", "-abspath", ".")
	fs := parseErrcheck(r.stdout+r.stderr, pkgDir)
	if len(fs) == 0 {
		return failedRun(r, pkgDir, "errcheck", "warning", r.stderr), nil
	}
	return fs, nil
}

func parseErrcheck(out, pkgDir string) []Finding {
	if toolBroken(out) {
		return brokenFinding("errcheck", pkgDir)
	}
	fs := textFindings(out, pkgDir, "errcheck", "warning")
	for i := range fs {
		fs[i].Msg = "unchecked error: " + strings.TrimSpace(fs[i].Msg)
	}
	return fs
}
