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

// isolateHome keeps fixtures away from the real cache dir and any
// .gochecks.json above the temp dir.
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

// The vet issue is in sibling.go; only clean.go is in scope.
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

// Install into a dir with an existing settings.json must keep the
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
		t.Fatalf("expected 2 PostToolUse entries (existing + gochecks); got %d: %+v", len(post), post)
	}
	foundGocheck := false
	for _, e := range post {
		m, _ := e.(map[string]any)
		hs, _ := m["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); cmd == hookCommand {
				foundGocheck = true
				if m["matcher"] != hookMatcher {
					t.Errorf("matcher: got %v, want %q", m["matcher"], hookMatcher)
				}
			}
		}
	}
	if !foundGocheck {
		t.Errorf("gochecks hook entry missing from merged settings: %s", data)
	}

	if start, _ := hooks["SessionStart"].([]any); len(start) != 1 {
		t.Errorf("expected 1 SessionStart entry; got %+v", start)
	}

	skillPath := filepath.Join(dir, ".claude", "skills", "go-current", "SKILL.md")
	if fi, err := os.Stat(skillPath); err != nil {
		t.Fatalf("SKILL.md missing after install: %v", err)
	} else if fi.Size() == 0 {
		t.Errorf("SKILL.md is empty")
	}

	docPath := filepath.Join(dir, ".claude", "gochecks.md")
	if fi, err := os.Stat(docPath); err != nil {
		t.Fatalf("gochecks.md missing after install: %v", err)
	} else if fi.Size() == 0 {
		t.Errorf("gochecks.md is empty")
	}

	// Re-running install adds nothing.
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
	if start2, _ := hooks2["SessionStart"].([]any); len(start2) != 1 {
		t.Errorf("second install duplicated SessionStart: got %d, want 1", len(start2))
	}
}

func TestCheckWithoutFilesIsAnError(t *testing.T) {
	isolateHome(t)
	if code := runCheck(nil); code != 2 {
		t.Errorf("check with no files: exit %d, want 2", code)
	}
	if code := runFix(nil); code != 2 {
		t.Errorf("fix with no files: exit %d, want 2", code)
	}
}

// --lint-config writes the embedded profile once and never overwrites a
// golangci config of any name.
func TestInstallLintConfig(t *testing.T) {
	dir := t.TempDir()
	_ = captureStdout(t, func() { runInstall([]string{"--dir", dir, "--no-hook", "--no-skill", "--no-doc", "--lint-config"}) })
	path := filepath.Join(dir, ".golangci.yml")
	got, err := os.ReadFile(path)
	if err != nil || string(got) != lintProfile {
		t.Fatalf("profile not written: %v", err)
	}
	if err := os.WriteFile(path, []byte("version: \"2\"\n# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() {
		runInstall([]string{"--dir", dir, "--no-hook", "--no-skill", "--no-doc", "--lint-config", "--force"})
	})
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "# mine") {
		t.Error("existing .golangci.yml was overwritten")
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, ".golangci.yaml"), []byte("version: \"2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() { runInstall([]string{"--dir", other, "--no-hook", "--no-skill", "--no-doc", "--lint-config"}) })
	if _, err := os.Stat(filepath.Join(other, ".golangci.yml")); err == nil {
		t.Error("wrote .golangci.yml beside an existing .golangci.yaml")
	}
}

func TestLintConfigSubcommand(t *testing.T) {
	out := captureStdout(t, func() { dispatch([]string{"lint-config"}) })
	if out != lintProfile || !strings.Contains(out, "version: \"2\"") {
		t.Error("lint-config should print the embedded profile verbatim")
	}
}
