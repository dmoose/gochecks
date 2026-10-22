package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// testEvent is the part of a go test -json event read here.
type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

var testFailLineRE = regexp.MustCompile(`^\s*([^:\s][^:]*?\.go):(\d+):\s*(.+)$`)

// runGoTest runs go test -json in pkgDir and reports failures as
// error findings.
func runGoTest(pkgDir string, timeout time.Duration) ([]Finding, error) {
	cmd := exec.Command("go", "test", "-json", "-timeout", timeout.String(), ".") //nolint:gosec // timeout is a parsed duration
	cmd.Dir = pkgDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()

	findings := parseTestEvents(stdout.String(), pkgDir)
	// A test binary that fails to build reports on stderr, not in JSON.
	if stderr.Len() > 0 && len(findings) == 0 {
		findings = append(findings, Finding{
			File:  pkgDir,
			Class: "error",
			Tool:  "test",
			Msg:   "test binary build failed: " + strings.TrimSpace(stderr.String()),
		})
	}
	return findings, nil
}

func parseTestEvents(out, pkgDir string) []Finding {
	type failure struct {
		test    string
		outputs []string
	}
	active := map[string]*failure{}
	var fails []*failure

	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Test == "" {
			continue
		}
		f, ok := active[ev.Test]
		if !ok {
			f = &failure{test: ev.Test}
			active[ev.Test] = f
		}
		switch ev.Action {
		case "output":
			f.outputs = append(f.outputs, ev.Output)
		case "fail":
			fails = append(fails, f)
			delete(active, ev.Test)
		case "pass", "skip":
			delete(active, ev.Test)
		}
	}

	var fs []Finding
	for _, f := range fails {
		fs = append(fs, failureFinding(f.test, f.outputs, pkgDir))
	}
	return fs
}

func failureFinding(test string, outputs []string, pkgDir string) Finding {
	// First t.Errorf/t.Fatalf line has the form "    file.go:line: message".
	for _, o := range outputs {
		o = strings.TrimRight(o, "\r\n")
		if m := testFailLineRE.FindStringSubmatch(o); m != nil {
			file := m[1]
			if !filepath.IsAbs(file) {
				file = filepath.Join(pkgDir, file)
			}
			ln, _ := strconv.Atoi(m[2])
			return Finding{
				File:  filepath.Clean(file),
				Line:  ln,
				Class: "error",
				Tool:  "test",
				Msg:   fmt.Sprintf("%s: %s", test, strings.TrimSpace(m[3])),
			}
		}
	}
	return Finding{
		File:  pkgDir,
		Class: "error",
		Tool:  "test",
		Msg:   test + " failed",
	}
}
