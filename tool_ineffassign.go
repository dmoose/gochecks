package main

import "os/exec"

func ineffassignAvailable() bool {
	_, err := exec.LookPath("ineffassign")
	return err == nil
}

// runIneffassign reports assignments whose value is never used.
func runIneffassign(pkgDir string) ([]Finding, error) {
	r := runTool(pkgDir, "ineffassign", ".")
	fs := parseIneffassign(r.stderr, pkgDir)
	if len(fs) == 0 {
		return failedRun(r, pkgDir, "ineffassign", "warning", r.stderr), nil
	}
	return fs, nil
}

func parseIneffassign(out, pkgDir string) []Finding {
	return textFindings(out, pkgDir, "ineffassign", "warning")
}
