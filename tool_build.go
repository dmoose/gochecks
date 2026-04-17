package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var buildLineRE = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.+)$`)

// runBuild compiles pkgDir and returns any build errors as findings.
// Output goes to /dev/null so no artifact is left behind.
func runBuild(pkgDir string) ([]Finding, error) {
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Dir = pkgDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	return parseBuild(stderr.String(), pkgDir), nil
}

func parseBuild(out, pkgDir string) []Finding {
	var fs []Finding
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := buildLineRE.FindStringSubmatch(line)
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
			Class: "error",
			Tool:  "build",
			Msg:   m[4],
		})
	}
	return fs
}
