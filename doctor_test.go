package main

import "testing"

func TestParseBuildInfo(t *testing.T) {
	out := "/Users/x/go/bin/staticcheck: go1.25.1\n\tpath\thonnef.co/go/tools/cmd/staticcheck\n\tmod\thonnef.co/go/tools\tv0.6.1\th1:abc=\n\tbuild\t-buildmode=exe\n"
	bi := parseBuildInfo(out)
	if bi.goVersion != "go1.25.1" || bi.module != "honnef.co/go/tools" || bi.version != "v0.6.1" {
		t.Errorf("got %+v", bi)
	}
}

// Output from staticcheck 2025.1.1, errcheck 1.10, and golangci-lint
// 2.11.3 under a Go 1.27 toolchain.
func TestToolBroken(t *testing.T) {
	broken := []string{
		`-: internal error in importing "internal/byteorder" (cannot decode "internal/byteorder", export data version 5 is greater than maximum supported version 2); please report an issue (compile)`,
		`2026/10/22 11:38:45 internal error: package "flag" without types was imported from "github.com/dmoose/gochecks"`,
		`could not import flag (/usr/local/go/src/flag/flag.go:86:2: could not import encoding (-: could not load export data: internal error in importing "encoding"`,
		`Error: can't load config: the Go language version (go1.26) used to build golangci-lint is lower than the targeted Go version (1.27)`,
	}
	for _, s := range broken {
		if !toolBroken(s) {
			t.Errorf("should be broken: %s", s)
		}
	}
	fine := []string{
		"",
		"main.go:12:2: this value of err is never used (SA4006)",
		"main.go:3:2:\tdefer f.Close()",
	}
	for _, s := range fine {
		if toolBroken(s) {
			t.Errorf("should be fine: %s", s)
		}
	}
}

func TestParseStaticcheckBrokenCollapsesToOneWarning(t *testing.T) {
	out := `-: internal error in importing "a" (export data version 5 is greater than maximum supported version 2) (compile)
-: internal error in importing "b" (export data version 5 is greater than maximum supported version 2) (compile)
`
	fs := parseStaticcheck(out, "/pkg")
	if len(fs) != 1 || fs[0].Class != "warning" || fs[0].Tool != "staticcheck" || fs[0].File != "/pkg" {
		t.Errorf("got %+v", fs)
	}
}

func TestToolReportVerdicts(t *testing.T) {
	cfg := defaultConfig()
	cases := []struct {
		r    toolReport
		want string
	}{
		{toolReport{spec: toolSpecs[1], path: "/x", installed: buildInfo{version: "v0.8.2"}, latest: "v0.8.2"}, "ok, latest"},
		{toolReport{spec: toolSpecs[1], path: "/x", installed: buildInfo{version: "v0.6.1", goVersion: "go1.25.1"}, latest: "v0.8.2", broken: true}, "BROKEN: built with go1.25.1, cannot read go1.27.3 export data; outdated, latest v0.8.2"},
		{toolReport{spec: toolSpecs[1]}, "MISSING"},
		{toolReport{spec: toolSpecs[4]}, "not installed; opt-in via linters=true"},
		{toolReport{spec: toolSpecs[0], path: "/x", installed: buildInfo{version: "v0.41.0"}}, "ok"},
	}
	for _, c := range cases {
		if got := verdictTool(c.r, cfg, "go1.27.3"); got != c.want {
			t.Errorf("verdict: got %q, want %q", got, c.want)
		}
	}
	if !cases[1].r.needsInstall(cfg) || cases[0].r.needsInstall(cfg) || cases[3].r.needsInstall(cfg) {
		t.Error("needsInstall disagrees with verdicts")
	}
}
