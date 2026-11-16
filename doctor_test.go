package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	goversion "go/version"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	spec := func(name string) toolSpec {
		for _, t := range toolSpecs {
			if t.name == name {
				return t
			}
		}
		panic(name)
	}
	cases := []struct {
		r    toolReport
		want string
	}{
		{toolReport{spec: spec("staticcheck"), path: "/x", installed: buildInfo{version: "v0.8.2"}, latest: "v0.8.2"}, "ok, latest"},
		{toolReport{spec: spec("staticcheck"), path: "/x", installed: buildInfo{version: "v0.6.1", goVersion: "go1.25.1"}, latest: "v0.8.2", broken: true}, "BROKEN: built with go1.25.1, cannot read go1.27.3 export data; outdated, latest v0.8.2"},
		{toolReport{spec: spec("staticcheck")}, "MISSING"},
		{toolReport{spec: spec("golangci-lint")}, "MISSING"},
		{toolReport{spec: spec("goimports"), path: "/x", installed: buildInfo{version: "v0.41.0"}}, "ok"},
	}
	for _, c := range cases {
		if got := verdictTool(c.r, cfg, "go1.27.3"); got != c.want {
			t.Errorf("verdict: got %q, want %q", got, c.want)
		}
	}
	if !cases[1].r.needsInstall(cfg) || cases[0].r.needsInstall(cfg) || !cases[3].r.needsInstall(cfg) {
		t.Error("needsInstall disagrees with verdicts")
	}
}

// A golangci-lint built with an older Go than the module's is broken.
// doctor suggests, and --fix runs, an install pinned to the module's
// Go, then says so if the rebuilt binary is still broken.
//
// The go on PATH is go1.24.0: up to 1.24, go install pkg@version
// ignores the module's go line and builds with the tool's own when it
// is newer, as go1.23 did with golangci-lint's 1.26 where this was
// found. Toolchains come from the real module cache through a file
// proxy, which also holds a fake golangci-lint module; the module
// cache is a temp dir and GOBIN is on the temp PATH.
//
// Skipped under -short (about 10s), and when go1.24.0 or go1.27.3 is
// not in the module cache, as on a fresh CI runner. A skip there is
// expected; it is not a pass.
func TestDoctorFixBuildsWithModuleGo(t *testing.T) {
	if testing.Short() {
		t.Skip("unpacks two toolchains")
	}
	hasGo(t)
	const localGo, toolGo, target = "go1.24.0", "go1.26.0", "go1.27.3"
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	toolchains := filepath.Join(strings.TrimSpace(string(out)), "cache", "download", "golang.org", "toolchain")
	// A toolchain skips the checksum database only when GOPROXY is one
	// file:// URL, so each proxy links the real cache's toolchains.
	newProxy := func(t *testing.T) string {
		proxy := t.TempDir()
		if err := os.MkdirAll(filepath.Join(proxy, "golang.org"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(toolchains, filepath.Join(proxy, "golang.org", "toolchain")); err != nil {
			t.Fatal(err)
		}
		return proxy
	}
	cache := t.TempDir()
	t.Cleanup(func() {
		// Toolchain dirs are read-only; let go remove them.
		c := exec.Command("go", "clean", "-modcache")
		c.Env = append(os.Environ(), "GOMODCACHE="+cache, "GOTOOLCHAIN=local", "GOFLAGS=")
		if out, err := c.CombinedOutput(); err != nil {
			t.Logf("go clean -modcache: %v: %s", err, out)
		}
	})
	t.Setenv("GOMODCACHE", cache)
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOROOT", "")
	t.Setenv("GOPROXY", "file://"+newProxy(t))
	cmd := exec.Command("go", "env", "GOROOT")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+localGo)
	out, err = cmd.Output()
	if err != nil {
		t.Skipf("%s not in the module cache: %v", localGo, err)
	}
	localBin := filepath.Join(strings.TrimSpace(string(out)), "bin")
	t.Setenv("GOTOOLCHAIN", "auto")

	var others []string
	for _, s := range toolSpecs {
		if s.name != "golangci-lint" {
			others = append(others, s.name)
		}
	}
	// works is the Go the published fake must be built with to work.
	for i, c := range []struct{ name, works string }{{"fixed", target}, {"still broken", "never"}} {
		t.Run(c.name, func(t *testing.T) {
			bin := fakeTools(t, "", others...)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+localBin)
			t.Setenv("GOBIN", bin)
			if got := buildBrokenTool(t, bin, "golangci-lint", target, glBrokenMsg); got != localGo {
				t.Fatalf("fake built with %s, want %s", got, localGo)
			}
			// One version per case: the module cache is shared.
			mod, ver := "github.com/golangci/golangci-lint/v2", fmt.Sprintf("v2.99.%d", i)
			proxy := newProxy(t)
			writeProxyModule(t, proxy, mod, ver, map[string]string{
				"go.mod":                    "module " + mod + "\n\ngo " + strings.TrimPrefix(toolGo, "go") + "\n",
				"cmd/golangci-lint/main.go": fakeToolSource(c.works, glBrokenMsg),
			})
			t.Setenv("GOPROXY", "file://"+proxy)

			t.Chdir(writeFixture(t, map[string]string{"go.mod": "module m\n\ngo " + strings.TrimPrefix(target, "go") + "\n"}))
			if v, _, _ := goToolchain(); v != target {
				t.Skipf("%s not in the module cache (got %q)", target, v)
			}

			var code int
			report := captureStdout(t, func() { code = runDoctor([]string{"--offline"}) })
			want := "GOTOOLCHAIN=" + target + " go install " + mod + "/cmd/golangci-lint@latest"
			if code != 1 || !strings.Contains(report, "BROKEN") || !strings.Contains(report, want) {
				t.Fatalf("exit %d; want BROKEN and %q in:\n%s", code, want, report)
			}

			report = captureStdout(t, func() { code = runDoctor([]string{"--offline", "--fix"}) })
			bi, err := readBuildInfo(filepath.Join(bin, "golangci-lint"))
			if err != nil {
				t.Fatal(err)
			}
			if bi.goVersion != target || bi.version != ver {
				t.Errorf("installed %s built with %s, want %s built with %s", bi.version, bi.goVersion, ver, target)
			}
			still := "golangci-lint is still broken after installing with " + target + ": " + filepath.Join(bin, "golangci-lint") + " was built with " + target
			if c.works == target && (code != 0 || strings.Contains(report, "still broken")) {
				t.Errorf("exit %d after --fix:\n%s", code, report)
			}
			if c.works != target && (code != 1 || !strings.Contains(report, still)) {
				t.Errorf("exit %d; want %q in:\n%s", code, still, report)
			}
		})
	}
}

// writeProxyModule lays out mod@ver in GOPROXY file form.
func writeProxyModule(t *testing.T, proxy, mod, ver string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(proxy, mod, "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for name, content := range files {
		w, err := zw.Create(mod + "@" + ver + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"list":        ver + "\n",
		ver + ".info": `{"Version":"` + ver + `","Time":"2026-01-01T00:00:00Z"}`,
		ver + ".mod":  files["go.mod"],
		ver + ".zip":  zb.String(),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// When the module's Go cannot be read, doctor says why, suggests only
// unpinned installs with a caveat, and --fix installs nothing; it does
// not fall back to the Go that built gochecks.
func TestDoctorUnreadableModuleGo(t *testing.T) {
	hasGo(t)
	var others []string
	for _, s := range toolSpecs {
		if s.name != "golangci-lint" {
			others = append(others, s.name)
		}
	}
	bin := fakeTools(t, "", others...)
	// Built from Go source, so its version and Go can be read.
	builtWith := buildBrokenTool(t, bin, "golangci-lint", "never", glBrokenMsg)
	broken := filepath.Join(bin, "golangci-lint")
	script, err := os.ReadFile(broken)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOBIN", bin)
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	t.Chdir(writeFixture(t, map[string]string{"go.mod": "module m\n\ngo 1.99.0\n"}))

	for _, args := range [][]string{{"--offline"}, {"--offline", "--fix"}} {
		var code int
		report := captureStdout(t, func() { code = runDoctor(args) })
		for _, want := range []string{
			"cannot read the module's Go: go: download go1.99.0",
			"(devel) (built " + builtWith + ")",
			"the module's Go could not be read, so --fix installs nothing.",
			"\n  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest\n",
		} {
			if !strings.Contains(report, want) {
				t.Errorf("%v: missing %q in:\n%s", args, want, report)
			}
		}
		if code != 1 || strings.Contains(report, "GOTOOLCHAIN=") {
			t.Errorf("%v: exit %d, want 1 and no GOTOOLCHAIN in:\n%s", args, code, report)
		}
		if got, _ := os.ReadFile(broken); !bytes.Equal(got, script) {
			t.Errorf("%v: golangci-lint was replaced", args)
		}
	}

	// Nothing to install: still exit 1, the module's Go is unknown.
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var code int
	report := captureStdout(t, func() { code = runDoctor([]string{"--offline"}) })
	if code != 1 || !strings.Contains(report, "cannot read the module's Go") {
		t.Errorf("all tools fine: exit %d, want 1, in:\n%s", code, report)
	}
}

// A newer Go patch than the one the module pins, with the pin running,
// is shown but is not attention. A go line naming only a language
// version, or a pin newer than the Go running, still is. go.dev is
// faked; module lookups fail fast with GOPROXY=off.
func TestDoctorPinnedGoPatch(t *testing.T) {
	hasGo(t)
	cmd := exec.Command("go", "env", "GOVERSION")
	cmd.Dir = writeFixture(t, map[string]string{"go.mod": "module probe\n\ngo 1.21\n"})
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	local := strings.TrimSpace(string(out))
	if goversion.Lang(local) == local {
		t.Skipf("local Go %s is not a patch release", local)
	}
	lang := strings.TrimPrefix(goversion.Lang(local), "go")
	exact := strings.TrimPrefix(local, "go")
	const latest = "go1.99.0"
	old := latestGoRelease
	latestGoRelease = func() string { return latest }
	t.Cleanup(func() { latestGoRelease = old })

	var names []string
	for _, s := range toolSpecs {
		names = append(names, s.name)
	}
	fakeTools(t, "", names...)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "")

	cases := []struct {
		name, toolchain string
		files           map[string]string
		want            string
		code            int
	}{
		{"go line pins", "auto", map[string]string{"go.mod": "module m\n\ngo " + exact + "\n"}, "(pinned by go.mod)", 0},
		{"language only", "auto", map[string]string{"go.mod": "module m\n\ngo " + lang + "\n"}, "(https://go.dev/dl)", 1},
		{"toolchain line pins", "auto", map[string]string{"go.mod": "module m\n\ngo " + lang + "\n\ntoolchain " + local + "\n"}, "(pinned by go.mod)", 0},
		{"running older than pin", "local", map[string]string{"go.mod": "module m\n\ngo " + lang + "\n\ntoolchain go" + lang + ".99\n"}, "(https://go.dev/dl)", 1},
		{"go.work pins", "auto", map[string]string{
			"go.work": "go " + exact + "\n\nuse .\n",
			"go.mod":  "module m\n\ngo " + lang + "\n",
		}, "(pinned by go.work)", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GOTOOLCHAIN", c.toolchain)
			t.Chdir(writeFixture(t, c.files))
			var code int
			report := captureStdout(t, func() { code = runDoctor(nil) })
			want := local + "  "
			if !strings.Contains(report, want) || !strings.Contains(report, "outdated, latest "+latest+" "+c.want) || code != c.code {
				t.Errorf("exit %d, want %d and %q in:\n%s", code, c.code, c.want, report)
			}
		})
	}
}
