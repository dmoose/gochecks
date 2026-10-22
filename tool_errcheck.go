package main

import (
	"os/exec"
	"strings"
)

func errcheckAvailable() bool {
	_, err := exec.LookPath("errcheck")
	return err == nil
}

// runErrcheck reports unchecked errors. errcheck prints
// "file:line:col:\t<source>", so the message is the offending line.
func runErrcheck(pkgDir string) ([]Finding, error) {
	stdout, stderr := runTool(pkgDir, "errcheck", "-abspath", ".")
	return parseErrcheck(stdout+stderr, pkgDir), nil
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
