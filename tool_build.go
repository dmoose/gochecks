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

// runBuild compiles pkgDir and returns compile errors as findings. A
// failed build with no positioned error, such as a module the go.work
// does not list, is one error naming why.
func runBuild(pkgDir string) []Finding {
	r := runTool(pkgDir, "go", "build", "-json", "-o", os.DevNull, ".")
	// Errors before the build starts are plain text on stderr.
	fs := append(parseBuildEvents(r.stdout, pkgDir), parseBuild(r.stderr, pkgDir)...)
	if len(fs) > 0 {
		return fs
	}
	return failedRun(r, pkgDir, "build", "error", buildEventText(r.stdout)+r.stderr)
}

// parseBuildEvents joins the build-output events, which together are
// the text build's output.
func parseBuildEvents(out, pkgDir string) []Finding {
	return parseBuild(buildEventText(out), pkgDir)
}

func buildEventText(out string) string {
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
	return text.String()
}

func parseBuild(out, pkgDir string) []Finding {
	return textFindings(out, pkgDir, "build", "error")
}
