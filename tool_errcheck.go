package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// errcheck format: "file:line:col:\t<source>"
// (The tab before the source is the distinguishing feature vs vet.)
var errcheckLineRE = regexp.MustCompile(`^(.+?):(\d+):(\d+):\t(.+)$`)

func errcheckAvailable() bool {
	_, err := exec.LookPath("errcheck")
	return err == nil
}

func runErrcheck(pkgDir string) ([]Finding, error) {
	cmd := exec.Command("errcheck", "./...")
	cmd.Dir = pkgDir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// errcheck exits non-zero on findings; expected, not an error.
	_ = cmd.Run()
	return parseErrcheck(out.String(), pkgDir), nil
}

func parseErrcheck(out, pkgDir string) []Finding {
	if toolBroken(out) {
		return brokenFinding("errcheck", pkgDir)
	}
	var fs []Finding
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		m := errcheckLineRE.FindStringSubmatch(line)
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
			Tool:  "errcheck",
			Msg:   "unchecked error: " + strings.TrimSpace(m[4]),
		})
	}
	return fs
}
