package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tree with a go.work root that is not a repo, one wired module, one
// unwired module, one Makefile under the old name, and launch points
// in places a session does not start.
func TestDoctorScan(t *testing.T) {
	isolateHome(t)
	root := writeFixture(t, map[string]string{
		"ws/go.work":               "go 1.26\n\nuse ./wired\n",
		"ws/wired/go.mod":          "module example.com/wired\n",
		"ws/bare/go.mod":           "module example.com/bare\n",
		"old/go.mod":               "module example.com/old\n",
		"old/Makefile":             "# gocheck in a comment is fine\ncheck:\n\tgocheck check ./...\n",
		"ws/wired/testdata/go.mod": "module fixture\n",
		"ws/node_modules/x/go.mod": "module x\n",
		"ws/.hidden/go.mod":        "module hidden\n",
		"notgo/README.md":          "# hi\n",
	})
	for _, dir := range []string{"ws/wired", "old"} {
		if code := runInstall([]string{"--dir", filepath.Join(root, dir), "--no-skill", "--no-doc"}); code != 0 {
			t.Fatalf("install %s: exit %d", dir, code)
		}
	}

	var out bytes.Buffer
	if code := runScan(root, &out); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	got := out.String()
	for _, want := range []string{
		"ws/: no .claude/settings.json (run gochecks install)\n",
		"ws/bare/: no .claude/settings.json (run gochecks install)\n",
		"old/: Makefile runs gocheck, the old name; use gochecks\n",
		"4 launch point(s) under " + root + ", 3 problem(s)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, skip := range []string{"wired/:", "testdata", "node_modules", "hidden"} {
		if strings.Contains(got, skip) {
			t.Errorf("%q should not be reported:\n%s", skip, got)
		}
	}

	// Installing at the go.work root, which is not a repo, wires it.
	for _, dir := range []string{"ws", "ws/bare"} {
		if code := runInstall([]string{"--dir", filepath.Join(root, dir), "--no-skill", "--no-doc"}); code != 0 {
			t.Fatalf("install %s: exit %d", dir, code)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "old", "Makefile"), []byte("check:\n\tgochecks check ./...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := runScan(root, &out); code != 0 {
		t.Errorf("after wiring: exit %d, want 0:\n%s", code, out.String())
	}
}

// Settings that name only the old command, or have no PostToolUse
// entry, are reported.
func TestDoctorScanStaleSettings(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"a/go.mod":                "module a\n",
		"a/.claude/settings.json": `{"hooks":{"PostToolUse":[{"matcher":"Edit|Write","hooks":[{"type":"command","command":"gocheck hook"}]}]}}`,
		"b/go.mod":                "module b\n",
		"b/.claude/settings.json": `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"gochecks hook"}]}]}}`,
	})
	var out bytes.Buffer
	if code := runScan(root, &out); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	for _, want := range []string{
		"a/: no gochecks hook in .claude/settings.json (run gochecks install)\n",
		"a/: .claude/settings.json runs gocheck, the old name (run gochecks install --upgrade)\n",
		"b/: no gochecks hook in .claude/settings.json (run gochecks install)\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

// A session started at a go.work root gets the hook for a file in any
// listed module, with paths relative to the root.
func TestHookAtWorkspaceRoot(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	t.Setenv("GOWORK", "")
	dir := writeFixture(t, map[string]string{
		"go.work":  "go 1.26\n\nuse ./a\n",
		"a/go.mod": "module example.com/a\n\ngo 1.26\n",
		"a/bad.go": "package a\n\nimport \"fmt\"\n\nfunc Bad() {\n\tfmt.Printf(\"%d\\n\", \"x\")\n}\n",
	})
	var out, errw bytes.Buffer
	code := hook(strings.NewReader(hookEvent(dir, filepath.Join(dir, "a", "bad.go"))), &out, &errw)
	if code != 2 || !strings.Contains(errw.String(), "a/bad.go:6:") || !strings.Contains(errw.String(), "error/vet") {
		t.Errorf("want vet error on a/bad.go; code=%d stderr=%q", code, errw.String())
	}
}
