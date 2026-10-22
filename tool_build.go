package main

import (
	"encoding/json"
	"os"
	"strings"
)

// buildEvent is one line of `go build -json` (see go help buildjson).
type buildEvent struct {
	ImportPath string
	Action     string
	Output     string
}

// runBuild compiles pkgDir and returns compile errors as findings.
func runBuild(pkgDir string) ([]Finding, error) {
	stdout, stderr := runTool(pkgDir, "go", "build", "-json", "-o", os.DevNull, ".")
	// Early, serious errors still land on stderr as plain text.
	return append(parseBuildEvents(stdout, pkgDir), parseBuild(stderr, pkgDir)...), nil
}

// parseBuildEvents concatenates the build-output events, which together
// are exactly what the text build would have printed.
func parseBuildEvents(out, pkgDir string) []Finding {
	var text strings.Builder
	for line := range strings.SplitSeq(out, "\n") {
		var ev buildEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Action == "build-output" {
			text.WriteString(ev.Output)
		}
	}
	return parseBuild(text.String(), pkgDir)
}

func parseBuild(out, pkgDir string) []Finding {
	return textFindings(out, pkgDir, "build", "error")
}
