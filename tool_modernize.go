package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var (
	modernizeProgressRE = regexp.MustCompile(`applied (\d+) of \d+ fixes`)
	modernizeSkippedRE  = regexp.MustCompile(`(\d+) of \d+ fixes skipped`)
)

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

// modernizeConflicts reports fixes that `go fix` refused to apply
// because they overlap with other pending fixes. Called after
// runModernize has reached fixed point; returns a single suggestion
// per package summarizing the unresolved count. These conflicts are
// real modernize opportunities — the agent (or gopls MCP) needs to
// resolve them manually.
func modernizeConflicts(pkgDir string) []Finding {
	cmd := exec.Command("go", "fix", "-diff", ".")
	cmd.Dir = pkgDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	m := modernizeSkippedRE.FindStringSubmatch(stderr.String())
	if m == nil {
		return nil
	}
	n, _ := strconv.Atoi(m[1])
	if n == 0 {
		return nil
	}
	return []Finding{{
		File:  pkgDir,
		Class: "suggestion",
		Tool:  "modernize",
		Msg:   fmt.Sprintf("%d modernize fix(es) skipped due to conflicts; inspect with: go fix -diff .", n),
	}}
}
