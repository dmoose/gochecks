package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	findings := []Finding{
		{File: "/repo/pkg/a.go", Line: 3, Col: 4, Class: "warning", Tool: "staticcheck", Msg: "unused"},
		{File: "/elsewhere/b.go", Line: 1, Col: 1, Class: "suggestion", Tool: "modernize", Msg: "range"},
		{File: "/repo", Class: "suggestion", Tool: "modernize", Msg: "1 fix skipped"},
	}
	got := hookReport(findings, "/repo")
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
