package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	return capture(t, &os.Stdout, fn)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	return capture(t, &os.Stderr, fn)
}

func capture(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()
	old := *stream
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	*stream = w
	done := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()
	defer func() {
		*stream = old
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

// A path that does not exist is named on stderr and fails the run;
// the files that do exist are still checked and their findings
// reported.
func TestCheckNamesMissingFiles(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod":   fixtureGoMod,
		"messy.go": "package fixture\n\nfunc A()  int {return 1}\n",
	})
	messy := filepath.Join(dir, "messy.go")
	typo := filepath.Join(dir, "mesy.go")
	other := filepath.Join(dir, "sub", "gone.go")

	var code int
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			code = runCheck([]string{"--no-apply", messy, typo, "--changed", other})
		})
	})
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	for _, p := range []string{typo, other} {
		if !strings.Contains(stderr, "gochecks check: no such file: "+p+"\n") {
			t.Errorf("stderr does not name %s: %q", p, stderr)
		}
	}
	var gofmt bool
	for _, f := range parseJSONL(t, stdout) {
		gofmt = gofmt || f.File == messy && f.Class == "suggestion"
	}
	if !gofmt {
		t.Errorf("formatting finding in messy.go not reported: %q", stdout)
	}

	// Only missing files: still exit 2, nothing on stdout.
	stderr = captureStderr(t, func() {
		stdout = captureStdout(t, func() { code = runCheck([]string{typo}) })
	})
	if code != 2 || stdout != "" || !strings.Contains(stderr, typo) {
		t.Errorf("only missing: exit %d stdout %q stderr %q", code, stdout, stderr)
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

const glBrokenMsg = "Error: can't load config: the Go language version (go1.26) used to build golangci-lint is lower than the targeted Go version (1.27.3)"

// fakeTools puts scripts that print out on stderr and fail ahead of
// the go toolchain, with nothing else on PATH, and returns that dir.
func fakeTools(t *testing.T, out string, names ...string) string {
	t.Helper()
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho '" + out + "' >&2\nexit 3\n"
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(goPath))
	return bin
}

// buildBrokenTool builds bin/name from Go source: it prints msg and
// fails unless it was built with Go version works. It returns the Go
// it was built with.
func buildBrokenTool(t *testing.T, bin, name, works, msg string) string {
	t.Helper()
	src := writeFixture(t, map[string]string{
		"go.mod":  "module faketool\n\ngo 1.21\n",
		"main.go": fakeToolSource(works, msg),
	})
	cmd := exec.Command("go", "build", "-o", filepath.Join(bin, name), ".") //nolint:gosec // test fixture path
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake %s: %v\n%s", name, err, out)
	}
	ver := exec.Command("go", "env", "GOVERSION")
	ver.Dir = src
	out, err := ver.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func fakeToolSource(works, msg string) string {
	return fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	if runtime.Version() == %q {
		return
	}
	fmt.Fprintln(os.Stderr, %q)
	os.Exit(3)
}
`, works, msg)
}

// Two changed packages, each analyzer broken: one warning per tool,
// although the warning names no changed file.
func TestE2ECheckReportsBrokenTools(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod": fixtureGoMod,
		"a.go":   "package fixture\n\nfunc A() int { return 1 }\n",
		"b/b.go": "package b\n\nfunc B() int { return 2 }\n",
	})
	files := []string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b", "b.go")}
	off := false
	cases := []struct {
		name  string
		out   string
		tools []string
		cfg   Config
		built bool // golangci-lint is a Go binary; check names its Go
	}{
		{"golangci-lint", glBrokenMsg, nil, defaultConfig(), true},
		{"standalone", `-: internal error in importing "fmt" (export data version 5 is greater than maximum supported version 2)`, []string{"staticcheck", "errcheck"}, Config{Linters: &off}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin := fakeTools(t, c.out, c.tools...)
			tools, builtWith := c.tools, ""
			if c.built {
				builtWith = buildBrokenTool(t, bin, "golangci-lint", "never", c.out)
				tools = []string{"golangci-lint"}
			}
			var code int
			out := captureStdout(t, func() { code = runCheckWith(files, true, false, c.cfg) })
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			got := map[string]int{}
			for _, f := range parseJSONL(t, out) {
				if f.Class != "warning" || !strings.Contains(f.Msg, "run `gochecks doctor --fix`") {
					t.Errorf("unexpected finding %+v", f)
				}
				if builtWith != "" && !strings.HasPrefix(f.Msg, "built with "+builtWith+",") {
					t.Errorf("message should name %s: %q", builtWith, f.Msg)
				}
				got[f.Tool]++
			}
			for _, tool := range tools {
				if got[tool] != 1 {
					t.Errorf("%s: %d warnings, want 1; output:\n%s", tool, got[tool], out)
				}
			}
		})
	}
}

// fix applies to the files that exist, then names the missing ones and
// exits 2.
func TestFixNamesMissingFiles(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod":  fixtureGoMod,
		"loop.go": "package fixture\n\nfunc Sum(xs []int) int {\n\tn := 0\n\tfor i := 0; i < len(xs); i++ {\n\t\tn += xs[i]\n\t}\n\treturn n\n}\n",
	})
	target := filepath.Join(dir, "loop.go")
	typo := filepath.Join(dir, "lop.go")
	var code int
	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() { code = runFix([]string{"--changed", target + "," + typo}) })
	})
	if code != 2 || !strings.Contains(stderr, "gochecks fix: no such file: "+typo+"\n") {
		t.Errorf("exit %d, want 2 and %s named; stderr %q", code, typo, stderr)
	}
	if got, _ := os.ReadFile(target); !strings.Contains(string(got), "range xs") {
		t.Errorf("loop.go not modernized:\n%s", got)
	}
}

// A line-0 finding about a file prints the file; only a directory
// gets a trailing slash.
func TestTextLineZeroFileFinding(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	fakeTools(t, "")
	dir := writeFixture(t, map[string]string{
		"go.mod":   fixtureGoMod,
		"messy.go": "package fixture\n\nfunc A()  int {return 1}\n",
	})
	messy := filepath.Join(dir, "messy.go")
	out := captureStdout(t, func() { runCheckWith([]string{messy}, true, true, defaultConfig()) })
	if want := messy + " suggestion/gofmt: needs gofmt\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// A package of two files with modernize rewrites in each; b.go's
// reverse loop leaves a blank first line after go fix.
var modernizeFixture = map[string]string{
	"go.mod": "module fixture\n\ngo 1.27\n",
	"a.go": `package fixture

import "strings"

func Fields(s string) int {
	n := 0
	for _, f := range strings.Split(s, ",") {
		n += len(f)
	}
	return n
}
`,
	"b.go": `package fixture

func Has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func Last(xs []int) int {
	t := 0
	for i := len(xs) - 1; i >= 0; i-- {
		v := xs[i]
		t += v
	}
	return t
}
`,
}

// fix rewrites the files it is given and leaves the package's other
// files alone.
func TestFixRewritesOnlyNamedFiles(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, modernizeFixture)
	a, b := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")
	_ = captureStdout(t, func() { runFix([]string{a}) })
	if got, _ := os.ReadFile(a); !strings.Contains(string(got), "strings.SplitSeq(s, \",\")") {
		t.Errorf("a.go not modernized:\n%s", got)
	}
	if got, _ := os.ReadFile(b); string(got) != modernizeFixture["b.go"] {
		t.Errorf("b.go was not named but changed:\n%s", got)
	}
}

// check lists each rewrite fix would make in the named files, writes
// nothing, and lists none once fix has run.
func TestCheckReportsPendingModernize(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := writeFixture(t, modernizeFixture)
	b := filepath.Join(dir, "b.go")
	pending := func() []string {
		var got []string
		out := captureStdout(t, func() { runCheckWith([]string{b}, true, false, defaultConfig()) })
		for _, f := range parseJSONL(t, out) {
			if f.Tool == "modernize" {
				if f.Class != "suggestion" || f.File != b {
					t.Errorf("unexpected %+v", f)
				}
				got = append(got, fmt.Sprintf("%d:%d %s", f.Line, f.Col, f.Msg))
			}
		}
		return got
	}
	got := pending()
	want := []string{
		"4:2 Loop can be simplified using slices.Contains (slicescontains; gochecks fix applies it)",
		"14:6 backward loop over slice can be modernized using slices.Backward (slicesbackward; gochecks fix applies it)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("pending:\n got %q\nwant %q", got, want)
	}
	for name, content := range modernizeFixture {
		if data, _ := os.ReadFile(filepath.Join(dir, name)); string(data) != content {
			t.Errorf("check wrote %s", name)
		}
	}
	_ = captureStdout(t, func() { runFix([]string{b}) })
	if got := pending(); len(got) != 0 {
		t.Errorf("after fix, still pending: %q", got)
	}
}
