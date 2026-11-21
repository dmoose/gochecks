package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unlistedWorkspace is a go.work that lists a but not b, where b does
// not compile. go refuses to load b at all.
func unlistedWorkspace(t *testing.T) string {
	t.Helper()
	t.Setenv("GOWORK", "")
	return writeFixture(t, map[string]string{
		"go.work":  "go 1.26\n\nuse ./a\n",
		"a/go.mod": "module example.com/a\n\ngo 1.26\n",
		"a/a.go":   "package a\n",
		"b/go.mod": "module example.com/b\n\ngo 1.26\n",
		"b/b.go":   "package b\n\nfunc F() int { return \"x\" }\n",
		"b/c/c.go": "package c\n\nfunc G() int { return 1 }\n",
	})
}

// A module the go.work does not list is one build error naming the
// missing use line, never a clean pass.
func TestE2ECheckUnlistedWorkspaceModule(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := unlistedWorkspace(t)
	files := []string{filepath.Join(dir, "b", "b.go"), filepath.Join(dir, "b", "c", "c.go")}

	var code int
	out := captureStdout(t, func() { code = runCheckWith(files, true, false, defaultConfig()) })
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	fs := parseJSONL(t, out)
	if len(fs) != 1 {
		t.Fatalf("want one finding for the module, got %d:\n%s", len(fs), out)
	}
	f := fs[0]
	work := filepath.Join(dir, "go.work")
	if f.Class != "error" || f.Tool != "build" || f.File != filepath.Join(dir, "b") ||
		!strings.Contains(f.Msg, work) || !strings.Contains(f.Msg, "add `use ./b`") {
		t.Errorf("unexpected finding %+v", f)
	}
}

func TestHookUnlistedWorkspaceModuleBlocks(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := unlistedWorkspace(t)

	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, filepath.Join(dir, "b", "b.go"))), &out, &errw)
	if code != 2 {
		t.Fatalf("exit %d, want 2; stderr=%q", code, errw.String())
	}
	if !strings.Contains(errw.String(), "b/ error/build: module is not listed in") {
		t.Errorf("stderr should name the module; got %q", errw.String())
	}
}

// An analyzer that exits non-zero with output gochecks cannot parse is
// a warning, not a pass.
func TestE2ECheckReportsFailedAnalyzers(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod": fixtureGoMod,
		"a.go":   "package fixture\n\nfunc A() int { return 1 }\n",
	})
	tools := []string{"staticcheck", "errcheck", "ineffassign"}
	fakeTools(t, "something went wrong", tools...)
	off := false

	var code int
	out := captureStdout(t, func() {
		code = runCheckWith([]string{filepath.Join(dir, "a.go")}, true, false, Config{Linters: &off})
	})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	got := map[string]int{}
	for _, f := range parseJSONL(t, out) {
		if f.Class != "warning" || f.File != dir || f.Msg != "exited 3 without findings: something went wrong" {
			t.Errorf("unexpected finding %+v", f)
		}
		got[f.Tool]++
	}
	for _, tool := range tools {
		if got[tool] != 1 {
			t.Errorf("%s: %d warnings, want 1; output:\n%s", tool, got[tool], out)
		}
	}
}

// golangci-lint reports a load failure in the JSON report's Error, with
// no issues and exit 7.
func TestE2ECheckReportsGolangciLoadError(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod": fixtureGoMod,
		"a.go":   "package fixture\n\nfunc A() int { return 1 }\n",
	})
	bin := fakeTools(t, "")
	script := "#!/bin/sh\necho '{\"Issues\":[],\"Report\":{\"Error\":\"typechecking error: no packages\"}}'\nexit 7\n"
	if err := os.WriteFile(filepath.Join(bin, "golangci-lint"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runCheckWith([]string{filepath.Join(dir, "a.go")}, true, false, defaultConfig())
	})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	fs := parseJSONL(t, out)
	if len(fs) != 1 || fs[0].Tool != "golangci-lint" || fs[0].Class != "warning" ||
		fs[0].Msg != "exited 7 without findings: typechecking error: no packages" {
		t.Errorf("want one golangci-lint warning, got:\n%s", out)
	}
}

// A tool that cannot be started at all is reported, not skipped.
func TestFailedRunNamesStartError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	fs, err := runStaticcheck(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Class != "warning" || !strings.HasPrefix(fs[0].Msg, "did not run: ") ||
		!strings.Contains(fs[0].Msg, "staticcheck") {
		t.Errorf("want one did-not-run warning, got %+v", fs)
	}
}

// "linters": true asks for golangci-lint; without it, that is a
// warning on the config file.
func TestE2ECheckLintersOnWithoutGolangci(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod":         fixtureGoMod,
		".gochecks.json": `{"linters": true}`,
		"a.go":           "package fixture\n\nfunc A() int { return 1 }\n",
	})
	fakeTools(t, "")
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runCheckWith([]string{filepath.Join(dir, "a.go")}, true, false, cfg)
	})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	fs := parseJSONL(t, out)
	if len(fs) != 1 || fs[0].File != filepath.Join(dir, ".gochecks.json") || fs[0].Tool != "golangci-lint" ||
		!strings.Contains(fs[0].Msg, "not on PATH") {
		t.Errorf("want one warning on the config, got:\n%s", out)
	}

	// Unset, a missing golangci-lint is skipped as before.
	cfg.Linters = nil
	out = captureStdout(t, func() { code = runCheckWith([]string{filepath.Join(dir, "a.go")}, true, false, cfg) })
	if code != 0 || out != "" {
		t.Errorf("unset linters: exit %d, output %q", code, out)
	}
}
