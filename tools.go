package main

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// toolSpec describes an external tool: its binary, module, and
// whether it typechecks.
type toolSpec struct {
	name   string
	module string // module path, for @latest lookups
	pkg    string // package path, for go install
	// typechecks marks tools that read export data and so must be
	// rebuilt after a Go upgrade.
	typechecks bool
	// optIn names the config field that enables the tool, or "" when
	// presence is enough.
	optIn string
}

var toolSpecs = []toolSpec{
	{name: "goimports", module: "golang.org/x/tools", pkg: "golang.org/x/tools/cmd/goimports"},
	{name: "gofumpt", module: "mvdan.cc/gofumpt", pkg: "mvdan.cc/gofumpt"},
	{name: "staticcheck", module: "honnef.co/go/tools", pkg: "honnef.co/go/tools/cmd/staticcheck", typechecks: true},
	{name: "errcheck", module: "github.com/kisielk/errcheck", pkg: "github.com/kisielk/errcheck", typechecks: true},
	// ineffassign loads syntax only and reads no export data, so it has
	// no broken-tool probe.
	{name: "ineffassign", module: "github.com/gordonklaus/ineffassign", pkg: "github.com/gordonklaus/ineffassign"},
	{name: "golangci-lint", module: "github.com/golangci/golangci-lint/v2", pkg: "github.com/golangci/golangci-lint/v2/cmd/golangci-lint", typechecks: true},
	{name: "gopls", module: "golang.org/x/tools/gopls", pkg: "golang.org/x/tools/gopls"},
}

// toolBrokenRE matches what analyzers print when built against an
// older Go than the export data they are reading.
var toolBrokenRE = regexp.MustCompile(`export data version|internal error in importing|could not load export data|internal error: package .* without types|used to build golangci-lint is lower than the targeted Go version`)

func toolBroken(out string) bool {
	return toolBrokenRE.MatchString(out)
}

// brokenFinding replaces a tool's findings when its output shows it
// cannot read this toolchain. It is reported whatever the scope, once
// per tool; see collapseBroken.
func brokenFinding(tool, pkgDir string) []Finding {
	return []Finding{{
		File:   pkgDir,
		Class:  "warning",
		Tool:   tool,
		Msg:    brokenMsg(""),
		broken: true,
	}}
}

func brokenMsg(builtWith string) string {
	why := "cannot read this Go toolchain's export data"
	if builtWith != "" {
		why = "built with " + builtWith + ", " + why
	}
	return why + "; run `gochecks doctor --fix`"
}

// collapseBroken keeps one broken-tool finding per tool, the first by
// path, and names the Go the binary was built with.
func collapseBroken(findings []Finding) []Finding {
	first := map[string]int{}
	var out []Finding
	for _, f := range findings {
		if !f.broken {
			out = append(out, f)
			continue
		}
		i, seen := first[f.Tool]
		switch {
		case !seen:
			first[f.Tool] = len(out)
			out = append(out, f)
		case f.File < out[i].File:
			out[i] = f
		}
	}
	for _, i := range first {
		if path, err := exec.LookPath(out[i].Tool); err == nil {
			if bi, err := readBuildInfo(path); err == nil {
				out[i].Msg = brokenMsg(bi.goVersion)
			}
		}
	}
	return out
}

// buildInfo is what `go version -m` reports for an installed binary.
type buildInfo struct {
	goVersion string // toolchain the binary was built with, e.g. go1.26.1
	module    string
	version   string // module version, e.g. v0.8.2
}

// readBuildInfo runs outside any module, so it works where the
// module's own Go cannot be loaded.
func readBuildInfo(path string) (buildInfo, error) {
	cmd := exec.Command("go", "version", "-m", path)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return buildInfo{}, err
	}
	return parseBuildInfo(out.String()), nil
}

func parseBuildInfo(out string) buildInfo {
	var bi buildInfo
	for i, line := range strings.Split(out, "\n") {
		if i == 0 {
			// "<path>: go1.27.3"
			if _, v, ok := strings.Cut(line, ": "); ok {
				bi.goVersion = strings.TrimSpace(v)
			}
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) >= 4 && f[1] == "mod" {
			bi.module, bi.version = f[2], f[3]
		}
	}
	return bi
}
