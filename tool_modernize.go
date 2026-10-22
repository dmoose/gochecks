package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var modernizeProgressRE = regexp.MustCompile(`applied (\d+) of \d+ fixes`)

const modernizeMaxIterations = 10

// runModernize runs go fix until its output stops changing. go fix
// asks for a re-run ("applied N of M fixes") after each pass, and
// keeps asking when a remaining fix conflicts with another, so the
// stop condition is identical stderr on two consecutive runs.
func runModernize(pkgDir string) error {
	var prev string
	for i := range modernizeMaxIterations {
		cmd := exec.Command("go", "fix", ".")
		cmd.Dir = pkgDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		msg := stderr.String()
		if err == nil {
			return nil
		}
		progress := modernizeProgressRE.FindStringSubmatch(msg)
		if progress == nil {
			return fmt.Errorf("go fix: %w: %s", err, strings.TrimSpace(msg))
		}
		if i > 0 && msg == prev {
			return nil
		}
		prev = msg
	}
	return fmt.Errorf("go fix: exceeded %d iterations in %s", modernizeMaxIterations, pkgDir)
}

// modernizeConflicts lists the fixes go fix left unapplied, each at
// its own position.
func modernizeConflicts(pkgDir string) []Finding {
	stdout, _ := runTool(pkgDir, "go", "fix", "-json", ".")
	return parseModernize(stdout, pkgDir)
}

func parseModernize(out, pkgDir string) []Finding {
	return analysisFindings(parseAnalysisJSON(out), pkgDir, "modernize", "suggestion",
		func(analyzer, msg string) string {
			return msg + " (" + analyzer + "; conflicts with another fix, apply by hand)"
		})
}
