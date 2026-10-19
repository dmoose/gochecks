package main

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
)

// toolSpec describes an external tool gochecks can drive: how to find
// it, where it comes from, and whether it typechecks (and so can break
// when the Go toolchain moves ahead of the build it was compiled with).
type toolSpec struct {
	name   string
	module string // module path, for @latest lookups
	pkg    string // package path, for go install
	// typechecks is true for tools that load export data and therefore
	// need to be rebuilt after a Go toolchain upgrade.
	typechecks bool
	// optIn names the config field that enables the tool, or "" when it
	// is used whenever present.
	optIn string
}

var toolSpecs = []toolSpec{
	{name: "goimports", module: "golang.org/x/tools", pkg: "golang.org/x/tools/cmd/goimports"},
	{name: "staticcheck", module: "honnef.co/go/tools", pkg: "honnef.co/go/tools/cmd/staticcheck", typechecks: true},
	{name: "errcheck", module: "github.com/kisielk/errcheck", pkg: "github.com/kisielk/errcheck", typechecks: true},
	{name: "ineffassign", module: "github.com/gordonklaus/ineffassign", pkg: "github.com/gordonklaus/ineffassign"},
	{name: "golangci-lint", module: "github.com/golangci/golangci-lint/v2", pkg: "github.com/golangci/golangci-lint/v2/cmd/golangci-lint", typechecks: true, optIn: "linters"},
	{name: "gopls", module: "golang.org/x/tools/gopls", pkg: "golang.org/x/tools/gopls"},
}

// toolBrokenRE matches what analyzers print when they cannot read the
// export data of a newer toolchain than the one they were built with.
var toolBrokenRE = regexp.MustCompile(`export data version|internal error in importing|could not load export data|internal error: package .* without types|used to build golangci-lint is lower than the targeted Go version`)

func toolBroken(out string) bool {
	return toolBrokenRE.MatchString(out)
}

// brokenFinding is the single warning emitted in place of a tool's
// findings when its output shows it cannot analyze this toolchain.
func brokenFinding(tool, pkgDir string) []Finding {
	return []Finding{{
		File:  pkgDir,
		Class: "warning",
		Tool:  tool,
		Msg:   "cannot analyze code built with this Go toolchain; run `gochecks doctor`",
	}}
}

// buildInfo is what `go version -m` reports for an installed binary.
type buildInfo struct {
	goVersion string // toolchain the binary was built with, e.g. go1.26.1
	module    string
	version   string // module version, e.g. v0.8.2
}

func readBuildInfo(path string) (buildInfo, error) {
	cmd := exec.Command("go", "version", "-m", path)
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
