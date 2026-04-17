package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ineffassign format: "file:line:col: message"
var ineffassignLineRE = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.+)$`)

func ineffassignAvailable() bool {
	_, err := exec.LookPath("ineffassign")
	return err == nil
}

func runIneffassign(pkgDir string) ([]Finding, error) {
	cmd := exec.Command("ineffassign", "./...")
	cmd.Dir = pkgDir
	var stderr bytes.Buffer
	// ineffassign writes findings to stderr and exits 3 on any.
	cmd.Stderr = &stderr
	_ = cmd.Run()
	return parseIneffassign(stderr.String(), pkgDir), nil
}

func parseIneffassign(out, pkgDir string) []Finding {
	var fs []Finding
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := ineffassignLineRE.FindStringSubmatch(line)
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
			Tool:  "ineffassign",
			Msg:   m[4],
		})
	}
	return fs
}
