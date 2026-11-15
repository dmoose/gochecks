package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func hookEvent(cwd, file string) string {
	return fmt.Sprintf(`{"hook_event_name":"PostToolUse","tool_name":"Edit","cwd":%q,"tool_input":{"file_path":%q,"old_string":"a","new_string":"b"}}`, cwd, file)
}

func TestHookVetErrorGoesToStderr(t *testing.T) {
	hasGo(t)
	isolateHome(t)

	dir := writeFixture(t, map[string]string{
		"go.mod": fixtureGoMod,
		"bad.go": `package fixture

import "fmt"

func Bad() {
	fmt.Printf("%d\n", "not an int")
}
`,
	})
	target := filepath.Join(dir, "bad.go")

	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, target)), &out, &errw)
	if code != 2 {
		t.Fatalf("exit: got %d, want 2; stderr=%q", code, errw.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty on exit 2; got %q", out.String())
	}
	if !strings.Contains(errw.String(), "bad.go:6:") || !strings.Contains(errw.String(), "error/vet") {
		t.Errorf("stderr should carry the vet finding relative to cwd; got %q", errw.String())
	}
}

func TestHookCleanIsSilent(t *testing.T) {
	hasGo(t)
	isolateHome(t)

	dir := writeFixture(t, map[string]string{
		"go.mod":   fixtureGoMod,
		"clean.go": "package fixture\n\nfunc Clean() int { return 1 }\n",
	})
	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, filepath.Join(dir, "clean.go"))), &out, &errw)
	if code != 0 || out.Len() != 0 || errw.Len() != 0 {
		t.Errorf("want silent exit 0; got code=%d stdout=%q stderr=%q", code, out.String(), errw.String())
	}
}

func TestHookIgnoresNonGo(t *testing.T) {
	isolateHome(t)
	dir := writeFixture(t, map[string]string{"notes.md": "# hi\n"})
	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, filepath.Join(dir, "notes.md"))), &out, &errw)
	if code != 0 || out.Len() != 0 || errw.Len() != 0 {
		t.Errorf("want silent exit 0 for non-Go file; got code=%d stdout=%q stderr=%q", code, out.String(), errw.String())
	}
}

// Garbage on stdin is a misconfigured hook: non-blocking exit 1.
func TestHookBadInput(t *testing.T) {
	var out, errw bytes.Buffer
	if code := hook(strings.NewReader("not json"), &out, &errw); code != 1 {
		t.Errorf("exit: got %d, want 1", code)
	}
}

func TestHookReportShape(t *testing.T) {
	// The repo is a real dir: a line-0 finding gets its trailing slash
	// only when its path is a directory.
	repo := t.TempDir()
	findings := []Finding{
		{File: repo + "/pkg/a.go", Line: 3, Col: 4, Class: "warning", Tool: "staticcheck", Msg: "unused"},
		{File: "/elsewhere/b.go", Line: 1, Col: 1, Class: "suggestion", Tool: "modernize", Msg: "range"},
		{File: repo, Class: "suggestion", Tool: "modernize", Msg: "1 fix skipped"},
	}
	got := hookReport(findings, repo)
	want := "gochecks: 3 finding(s)\npkg/a.go:3:4 warning/staticcheck: unused\n/elsewhere/b.go:1:1 suggestion/modernize: range\n./ suggestion/modernize: 1 fix skipped\n"
	if got != want {
		t.Errorf("report:\n got %q\nwant %q", got, want)
	}

	var resp hookOutput
	resp.HookSpecificOutput.HookEventName = "PostToolUse"
	resp.HookSpecificOutput.AdditionalContext = got
	buf, _ := json.Marshal(resp)
	var m map[string]map[string]string
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatal(err)
	}
	if m["hookSpecificOutput"]["hookEventName"] != "PostToolUse" || m["hookSpecificOutput"]["additionalContext"] != got {
		t.Errorf("envelope: %s", buf)
	}
}

// A broken analyzer is not tied to the edited file; the hook still
// hands its warning to the model.
func TestHookReportsBrokenTool(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod":   fixtureGoMod,
		"clean.go": "package fixture\n\nfunc Clean() int { return 1 }\n",
	})
	bin := fakeTools(t, "")
	builtWith := buildBrokenTool(t, bin, "golangci-lint", "never", glBrokenMsg)

	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, filepath.Join(dir, "clean.go"))), &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, want 0; stderr=%q", code, errw.String())
	}
	var resp hookOutput
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("stdout is not a hook response: %v: %q", err, out.String())
	}
	want := "./ warning/golangci-lint: built with " + builtWith + ", cannot read this Go toolchain's export data; run `gochecks doctor --fix`"
	if !strings.Contains(resp.HookSpecificOutput.AdditionalContext, want) {
		t.Errorf("additionalContext missing %q; got %q", want, resp.HookSpecificOutput.AdditionalContext)
	}
}

// The hook names only files an edit wrote; one that is gone by the
// time the hook runs is not an error, and nothing is printed.
func TestHookIgnoresMissingFile(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{"go.mod": fixtureGoMod})
	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, filepath.Join(dir, "gone.go"))), &out, &errw)
	if code != 0 || out.Len() != 0 || errw.Len() != 0 {
		t.Errorf("want silent exit 0; got code=%d stdout=%q stderr=%q", code, out.String(), errw.String())
	}
}

// Exact hook text for a file finding and a broken-tool finding: the
// file has a position, the package directory a trailing slash.
func TestHookReportExactLines(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod":         fixtureGoMod,
		".gochecks.json": `{"linters": false}`,
		"p/a.go":         "package p\n\nfunc A() int { return 1 }\n",
	})
	file := filepath.Join(dir, "p", "a.go")
	bin := fakeTools(t, "")
	diag := `{"code":"SA4006","severity":"error","location":{"file":"` + file + `","line":3,"column":6},"message":"x is never used"}`
	if err := os.WriteFile(filepath.Join(bin, "staticcheck"), []byte("#!/bin/sh\necho '"+diag+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	builtWith := buildBrokenTool(t, bin, "errcheck", "never", `-: internal error in importing "fmt" (export data version 5 is greater than maximum supported version 2)`)

	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, file)), &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, want 0; stderr=%q", code, errw.String())
	}
	var resp hookOutput
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("stdout is not a hook response: %v: %q", err, out.String())
	}
	want := "gochecks: 2 finding(s)\n" +
		"p/a.go:3:6 warning/staticcheck: x is never used (SA4006)\n" +
		"p/ warning/errcheck: built with " + builtWith + ", cannot read this Go toolchain's export data; run `gochecks doctor --fix`\n"
	if got := resp.HookSpecificOutput.AdditionalContext; got != want {
		t.Errorf("additionalContext:\ngot  %q\nwant %q", got, want)
	}
}

// With auto_fix, the hook modernizes only the edited file and formats
// what go fix wrote: gofumpt drops the blank line the reverse-loop
// rewrite leaves. The other file's pending rewrites are not reported
// as conflicts.
func TestHookAutoFixScopedAndFormatted(t *testing.T) {
	hasGo(t)
	if _, err := exec.LookPath("gofumpt"); err != nil {
		t.Skip("gofumpt not on PATH")
	}
	isolateHome(t)
	files := maps.Clone(modernizeFixture)
	files[".gochecks.json"] = `{"auto_fix": true, "linters": false}`
	dir := writeFixture(t, files)
	b := filepath.Join(dir, "b.go")

	var out, errw bytes.Buffer
	if code := hook(strings.NewReader(hookEvent(dir, b)), &out, &errw); code != 0 {
		t.Fatalf("exit %d; stderr=%q", code, errw.String())
	}
	got, _ := os.ReadFile(b)
	if !strings.Contains(string(got), "range slices.Backward(xs) {\n\t\tt += v") {
		t.Errorf("b.go not modernized and formatted:\n%s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.go")); string(data) != modernizeFixture["a.go"] {
		t.Errorf("a.go was not edited but changed:\n%s", data)
	}
	if strings.Contains(out.String(), "a.go") {
		t.Errorf("a.go reported: %s", out.String())
	}
}
