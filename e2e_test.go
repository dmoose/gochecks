package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func hasGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
}

// isolateHome redirects os.UserCacheDir (and loadConfig's upward walk)
// so the e2e fixtures never touch the real user cache or see a stray
// .gocheck.json somewhere above /tmp.
func isolateHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CACHE_HOME", h)
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()
	defer func() {
		os.Stdout = old
	}()
	fn()
	_ = w.Close()
	return string(<-done)
}

func parseJSONL(t *testing.T, s string) []Finding {
	t.Helper()
	var out []Finding
	for line := range strings.SplitSeq(strings.TrimSpace(s), "\n") {
		if line == "" {
			continue
		}
		var f Finding
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Fatalf("bad JSONL line %q: %v", line, err)
		}
		out = append(out, f)
	}
	return out
}

const fixtureGoMod = "module fixture\n\ngo 1.26\n"

// TestE2ECheckReportsVetError covers the full check pipeline on a
// package with a known vet error: findings surface, exit code is 2,
// the finding is attributed to the right file/tool/class.
func TestE2ECheckReportsVetError(t *testing.T) {
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

	var code int
	out := captureStdout(t, func() {
		code = runCheckWith([]string{target}, false, false, defaultConfig())
	})
	if code != 2 {
		t.Errorf("exit code: got %d, want 2 (vet error present)", code)
	}

	findings := parseJSONL(t, out)
	var vet *Finding
	for i := range findings {
		if findings[i].Tool == "vet" {
			vet = &findings[i]
			break
		}
	}
	if vet == nil {
		t.Fatalf("expected a vet finding; got %+v", findings)
	}
	if vet.Class != "error" {
		t.Errorf("vet class: got %q, want error", vet.Class)
	}
	if vet.File != target {
		t.Errorf("vet file: got %q, want %q", vet.File, target)
	}
}

// TestE2ECheckScopeFiltersSiblingFiles exercises the --changed scope
// filter: a vet issue lives in sibling.go but only clean.go is in
// scope, so the finding must be suppressed even though the package
// run sees it.
func TestE2ECheckScopeFiltersSiblingFiles(t *testing.T) {
	hasGo(t)
	isolateHome(t)

	dir := writeFixture(t, map[string]string{
		"go.mod": fixtureGoMod,
		"clean.go": `package fixture

func Clean() int { return 1 }
`,
		"sibling.go": `package fixture

import "fmt"

func Sibling() {
	fmt.Printf("%d\n", "oops")
}
`,
	})
	clean := filepath.Join(dir, "clean.go")

	var code int
	out := captureStdout(t, func() {
		code = runCheckWith([]string{clean}, false, false, defaultConfig())
	})
	if code != 0 {
		t.Errorf("exit code: got %d, want 0 (sibling finding should be scope-filtered)", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected no findings; got: %s", out)
	}
}

// TestE2EFixAppliesModernize proves the fix subcommand actually
// rewrites source: a classic counter-loop should become a range loop.
func TestE2EFixAppliesModernize(t *testing.T) {
	hasGo(t)
	isolateHome(t)

	original := `package fixture

func Sum(xs []int) int {
	n := 0
	for i := 0; i < len(xs); i++ {
		n += xs[i]
	}
	return n
}
`
	dir := writeFixture(t, map[string]string{
		"go.mod":  fixtureGoMod,
		"loop.go": original,
	})
	target := filepath.Join(dir, "loop.go")

	_ = captureStdout(t, func() {
		runFix([]string{"--changed", target})
	})

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "range xs") {
		t.Errorf("expected modernize to rewrite loop to `range xs`; file:\n%s", got)
	}
	if strings.Contains(string(got), "i < len(xs)") {
		t.Errorf("counter-style loop still present after fix:\n%s", got)
	}
}

// TestE2EInstallRoundTrip walks the install path: empty dir → hook
// and skill land; existing settings.json → merge without clobbering
// unrelated keys.
func TestE2EInstallRoundTrip(t *testing.T) {
	dir := t.TempDir()

	preExisting := `{
  "theme": "dark",
  "hooks": {
    "PostToolUse": [
      {"matcher": "Read", "hooks": [{"type": "command", "command": "echo unrelated"}]}
    ]
  }
}
`
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(preExisting), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = captureStdout(t, func() {
		runInstall([]string{"--dir", dir})
	})

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json is not valid JSON after install: %v", err)
	}
	if settings["theme"] != "dark" {
		t.Errorf("unrelated key `theme` lost during merge: %v", settings["theme"])
	}
	hooks, _ := settings["hooks"].(map[string]any)
	post, _ := hooks["PostToolUse"].([]any)
	if len(post) != 2 {
		t.Fatalf("expected 2 PostToolUse entries (existing + gocheck); got %d: %+v", len(post), post)
	}
	// The gocheck entry must be present; match by command substring.
	foundGocheck := false
	for _, e := range post {
		m, _ := e.(map[string]any)
		hs, _ := m["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, "gocheck check") {
				foundGocheck = true
			}
		}
	}
	if !foundGocheck {
		t.Errorf("gocheck hook entry missing from merged settings: %s", data)
	}

	skillPath := filepath.Join(dir, ".claude", "skills", "go-current", "SKILL.md")
	if fi, err := os.Stat(skillPath); err != nil {
		t.Fatalf("SKILL.md missing after install: %v", err)
	} else if fi.Size() == 0 {
		t.Errorf("SKILL.md is empty")
	}

	docPath := filepath.Join(dir, ".claude", "gocheck.md")
	if fi, err := os.Stat(docPath); err != nil {
		t.Fatalf("gocheck.md missing after install: %v", err)
	} else if fi.Size() == 0 {
		t.Errorf("gocheck.md is empty")
	}

	// Re-running install is idempotent: no error, no duplicate gocheck entry.
	_ = captureStdout(t, func() {
		runInstall([]string{"--dir", dir})
	})
	data2, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	var settings2 map[string]any
	_ = json.Unmarshal(data2, &settings2)
	hooks2, _ := settings2["hooks"].(map[string]any)
	post2, _ := hooks2["PostToolUse"].([]any)
	if len(post2) != 2 {
		t.Errorf("second install duplicated entries: got %d, want 2", len(post2))
	}
}
