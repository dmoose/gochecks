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

// runModernize iterates `go fix .` until the output stabilizes.
//
// Modernize is multi-pass: later fixes can depend on earlier ones
// landing first, and `go fix` signals this by exiting non-zero with
// "applied N of M fixes; re-run". But it also reports the same
// message indefinitely when one fix conflicts and can never be
// applied — so we can't trust the message alone. We loop until two
// consecutive runs produce the same stderr (fixed point), at which
// point whatever is left is unreachable and we treat the run as done.
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

// modernizeConflicts lists the fixes `go fix` left unapplied once
// runModernize reached its fixed point: fixes that overlap another
// pending fix. Each is a real modernize opportunity the agent has to
// resolve by hand, so each is reported at its own position.
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
