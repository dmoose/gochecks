package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var vetLineRE = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.+)$`)

func runVet(pkgDir string) ([]Finding, error) {
	cmd := exec.Command("go", "vet", ".")
	cmd.Dir = pkgDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// go vet exits non-zero on findings; that's expected, not an error.
	_ = cmd.Run()
	return parseVet(stderr.String(), pkgDir), nil
}

func parseVet(out, pkgDir string) []Finding {
	var fs []Finding
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := vetLineRE.FindStringSubmatch(line)
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
			Tool:  "vet",
			Msg:   m[4],
		})
	}
	return fs
}

// Go's own build cache handles vet caching with correct transitive
// invalidation — so we call runVet directly rather than wrap it.

func filterScope(findings []Finding, scope map[string]bool) []Finding {
	var out []Finding
	for _, f := range findings {
		if scope[f.File] {
			out = append(out, f)
		}
	}
	return out
}
