package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// staticcheck format: "file:line:col: message (CHECKID)"
var staticcheckLineRE = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s+(.+)$`)

// staticcheckAvailable returns true if the binary is on PATH. The
// absence of staticcheck is not an error — it's an optional extra
// layer that only runs if the user has installed it.
func staticcheckAvailable() bool {
	_, err := exec.LookPath("staticcheck")
	return err == nil
}

func runStaticcheck(pkgDir string) ([]Finding, error) {
	cmd := exec.Command("staticcheck", ".")
	cmd.Dir = pkgDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// staticcheck exits non-zero on findings; not an error for us.
	_ = cmd.Run()
	return parseStaticcheck(stdout.String(), pkgDir), nil
}

func parseStaticcheck(out, pkgDir string) []Finding {
	if toolBroken(out) {
		return brokenFinding("staticcheck", pkgDir)
	}
	var fs []Finding
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := staticcheckLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		file := m[1]
		if !filepath.IsAbs(file) {
			file = filepath.Join(pkgDir, file)
		}
		file = filepath.Clean(file)
		ln, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		fs = append(fs, Finding{
			File:  file,
			Line:  ln,
			Col:   col,
			Class: "warning",
			Tool:  "staticcheck",
			Msg:   m[4],
		})
	}
	return fs
}
