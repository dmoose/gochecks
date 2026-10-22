package main

import "os/exec"

func ineffassignAvailable() bool {
	_, err := exec.LookPath("ineffassign")
	return err == nil
}

// runIneffassign reports assignments whose value is never used.
func runIneffassign(pkgDir string) ([]Finding, error) {
	_, stderr := runTool(pkgDir, "ineffassign", ".")
	return parseIneffassign(stderr, pkgDir), nil
}

func parseIneffassign(out, pkgDir string) []Finding {
	return textFindings(out, pkgDir, "ineffassign", "warning")
}
