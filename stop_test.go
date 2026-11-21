package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func stopEvent(session, cwd string, active bool) string {
	return fmt.Sprintf(`{"hook_event_name":"Stop","session_id":%q,"transcript_path":"/dev/null","cwd":%q,"permission_mode":"default","stop_hook_active":%t,"last_assistant_message":"Done."}`, session, cwd, active)
}

func promptEvent(session, cwd string) string {
	return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":%q,"transcript_path":"/dev/null","cwd":%q,"permission_mode":"default","prompt":"next"}`, session, cwd)
}

func startEvent(session, cwd string) string {
	return fmt.Sprintf(`{"hook_event_name":"SessionStart","session_id":%q,"transcript_path":"/dev/null","cwd":%q,"source":"startup"}`, session, cwd)
}

type hookResult struct {
	code        int
	out, stderr string
}

func runHookEvent(t *testing.T, ev string) hookResult {
	t.Helper()
	var out, errw bytes.Buffer
	code := hook(strings.NewReader(ev), &out, &errw)
	return hookResult{code, out.String(), errw.String()}
}

// gitRepo writes files into a new repo and commits them.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := writeFixture(t, files)
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init")
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil { //nolint:gosec // test fixture
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// edit writes a file the way a Bash edit does: no PostToolUse.
func edit(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stopFixture turns golangci-lint off, so the test does not pay its
// startup. Installed analyzers may still repeat vet's findings, so the
// tests below assert findings, not counts.
func stopFixture() map[string]string {
	return map[string]string{"go.mod": fixtureGoMod, ".gochecks.json": `{"linters": false}`, "a.go": cleanSrc}
}

const (
	cleanSrc = "package fixture\n\nfunc A() int { return 1 }\n"
	vetBad   = "package fixture\n\nimport \"fmt\"\n\nfunc A() int {\n\tfmt.Printf(\"%d\\n\", \"x\")\n\treturn 1\n}\n"
)

func TestStopBlocksOnChangedFiles(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := gitRepo(t, stopFixture())

	if r := runHookEvent(t, stopEvent("s1", dir, false)); r.code != 0 || r.out != "" || r.stderr != "" {
		t.Fatalf("clean tree: %+v", r)
	}

	// A tracked file edited outside Edit/Write, and a new untracked one.
	edit(t, filepath.Join(dir, "a.go"), vetBad)
	edit(t, filepath.Join(dir, "sub", "b.go"), "package sub\n\nfunc B() int { return \"x\" }\n")
	r := runHookEvent(t, stopEvent("s1", dir, false))
	if r.code != 2 {
		t.Fatalf("exit %d, want 2: %+v", r.code, r)
	}
	for _, want := range []string{
		"blocking finding(s) in files changed since HEAD",
		"a.go:6:", "error/vet",
		"sub/b.go:3:", "error/build",
		"Touching a file means owning every finding",
		`gochecks allow "<reason>"`,
	} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, r.stderr)
		}
	}

	// Committed, nothing differs from HEAD: the stop passes.
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "bad")
	if r := runHookEvent(t, stopEvent("s1", dir, false)); r.code != 0 {
		t.Errorf("after commit: %+v", r)
	}
}

// A warning blocks as an error does.
func TestStopBlocksOnWarning(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := gitRepo(t, stopFixture())
	diag := fmt.Sprintf(`{"code":"SA4006","severity":"error","location":{"file":%q,"line":3,"column":1},"message":"value never used"}`, filepath.Join(dir, "a.go"))
	gitPath, _ := exec.LookPath("git")
	fakeTools(t, diag, "staticcheck")
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Dir(gitPath))
	edit(t, filepath.Join(dir, "a.go"), "package fixture\n\nfunc A() int { return 2 }\n")

	r := runHookEvent(t, stopEvent("w1", dir, false))
	if r.code != 2 || !strings.Contains(r.stderr, "a.go:3:1 warning/staticcheck: value never used (SA4006)") {
		t.Errorf("want a blocking staticcheck warning: %+v", r)
	}
}

// Suggestions do not block. Context added at Stop would keep the turn
// going, so they come with the next prompt, once.
func TestStopSuggestionsGoToNextPrompt(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := gitRepo(t, stopFixture())
	edit(t, filepath.Join(dir, "a.go"), "package fixture\n\nfunc A()  int {return 1}\n")

	if r := runHookEvent(t, stopEvent("p1", dir, false)); r.code != 0 || r.out != "" || r.stderr != "" {
		t.Fatalf("suggestions must not block or add Stop context: %+v", r)
	}
	r := runHookEvent(t, promptEvent("p1", dir))
	if r.code != 0 || !strings.Contains(r.out, `"hookEventName":"UserPromptSubmit"`) || !strings.Contains(r.out, "a.go suggestion/") {
		t.Errorf("want the suggestion as prompt context: %+v", r)
	}
	if r := runHookEvent(t, promptEvent("p1", dir)); r.out != "" {
		t.Errorf("suggestions are passed on once: %+v", r)
	}
}

// Blocked twice on the same set with stop_hook_active: let through,
// then remind on every prompt and the next session until a stop passes.
func TestStopStuckLetsThroughAndReminds(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := gitRepo(t, stopFixture())
	edit(t, filepath.Join(dir, "a.go"), vetBad)

	if r := runHookEvent(t, stopEvent("s1", dir, false)); r.code != 2 {
		t.Fatalf("first stop: %+v", r)
	}
	if r := runHookEvent(t, stopEvent("s1", dir, true)); r.code != 0 {
		t.Fatalf("stuck stop should pass: %+v", r)
	}
	want := " blocking finding(s) from the last stop still stand in " + dir
	for _, ev := range []string{promptEvent("s1", dir), promptEvent("s1", dir), startEvent("s2", dir)} {
		if r := runHookEvent(t, ev); !strings.Contains(r.out, want) {
			t.Errorf("want reminder for %s, got %+v", ev, r)
		}
	}
	if r := runHookEvent(t, startEvent("s3", t.TempDir())); r.out != "" {
		t.Errorf("another directory gets no reminder: %+v", r)
	}

	// Progress (a changed set) blocks again even with stop_hook_active.
	edit(t, filepath.Join(dir, "b.go"), "package fixture\n\nfunc B() int { return \"x\" }\n")
	if r := runHookEvent(t, stopEvent("s1", dir, true)); r.code != 2 {
		t.Errorf("changed set should block: %+v", r)
	}

	// Fixed: the stop passes and the reminders end.
	edit(t, filepath.Join(dir, "a.go"), cleanSrc)
	_ = os.Remove(filepath.Join(dir, "b.go"))
	if r := runHookEvent(t, stopEvent("s1", dir, false)); r.code != 0 {
		t.Fatalf("fixed: %+v", r)
	}
	if r := runHookEvent(t, promptEvent("s1", dir)); r.out != "" {
		t.Errorf("no reminder after a clean stop: %+v", r)
	}
	if r := runHookEvent(t, startEvent("s4", dir)); strings.Contains(r.out, "still stand") {
		t.Errorf("no session reminder after a clean stop: %+v", r)
	}

	// A clean stop in a later session ends an earlier session's reminder.
	edit(t, filepath.Join(dir, "a.go"), vetBad)
	runHookEvent(t, stopEvent("s5", dir, false))
	runHookEvent(t, stopEvent("s5", dir, true))
	if r := runHookEvent(t, startEvent("s6", dir)); !strings.Contains(r.out, "still stand") {
		t.Fatalf("s5 left findings standing: %+v", r)
	}
	edit(t, filepath.Join(dir, "a.go"), cleanSrc)
	if r := runHookEvent(t, stopEvent("s6", dir, false)); r.code != 0 {
		t.Fatalf("s6 clean stop: %+v", r)
	}
	if r := runHookEvent(t, startEvent("s7", dir)); strings.Contains(r.out, "still stand") {
		t.Errorf("s5's reminder outlived a clean stop in s6: %+v", r)
	}
}

func TestAllowReleasesOneStop(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := gitRepo(t, stopFixture())
	edit(t, filepath.Join(dir, "a.go"), vetBad)
	t.Chdir(dir)

	if code := runAllow([]string{"no block yet"}); code != 2 {
		t.Errorf("allow with nothing blocked: exit %d, want 2", code)
	}
	if r := runHookEvent(t, stopEvent("s1", dir, false)); r.code != 2 {
		t.Fatalf("block: %+v", r)
	}
	if code := runAllow(nil); code != 2 {
		t.Errorf("allow without a reason: exit %d, want 2", code)
	}
	var code int
	out := captureStdout(t, func() { code = runAllow([]string{"vet is wrong about this one"}) })
	if code != 0 || !strings.Contains(out, " finding(s) in "+dir+" (session s1)") {
		t.Fatalf("allow: exit %d, %q", code, out)
	}
	if r := runHookEvent(t, stopEvent("s1", dir, true)); r.code != 0 {
		t.Errorf("allowed stop should pass: %+v", r)
	}
	if r := runHookEvent(t, stopEvent("s1", dir, false)); r.code != 2 {
		t.Errorf("the pass is one-shot: %+v", r)
	}

	// A grant for one set does not release a changed one.
	_ = captureStdout(t, func() { runAllow([]string{"again"}) })
	edit(t, filepath.Join(dir, "b.go"), "package fixture\n\nfunc B() int { return \"x\" }\n")
	if r := runHookEvent(t, stopEvent("s1", dir, false)); r.code != 2 {
		t.Errorf("changed set after allow should block: %+v", r)
	}

	base, _ := cacheDir()
	log, err := os.ReadFile(filepath.Join(base, "stop", "allow.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`granted session=s1`, `reason="vet is wrong about this one"`, `used session=s1`} {
		if !strings.Contains(string(log), want) {
			t.Errorf("allow.log missing %q:\n%s", want, log)
		}
	}
}

// A session at a go.work root that is not a repo checks the repo of
// each module the go.work uses.
func TestStopAtWorkspaceRoot(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	t.Setenv("GOWORK", "")
	ws := t.TempDir()
	a := gitRepo(t, map[string]string{"go.mod": "module example.com/a\n\ngo 1.26\n", "a.go": "package a\n"})
	link := filepath.Join(ws, "a")
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	edit(t, filepath.Join(ws, "go.work"), "go 1.26\n\nuse ./a\n")
	edit(t, filepath.Join(a, "a.go"), "package a\n\nfunc F() int { return \"x\" }\n")

	r := runHookEvent(t, stopEvent("s1", ws, false))
	if r.code != 2 || !strings.Contains(r.stderr, "error/build") || !strings.Contains(r.stderr, "a.go:3:") {
		t.Errorf("want the module's build error: %+v", r)
	}
}

func TestInstallStop(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	for range 2 {
		if code := runInstall([]string{"--dir", dir, "--stop", "--no-skill", "--no-doc"}); code != 0 {
			t.Fatalf("install: exit %d", code)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks       map[string][]any
		Permissions struct{ Deny []string }
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"PostToolUse", "SessionStart", "Stop", "UserPromptSubmit"} {
		if len(s.Hooks[ev]) != 1 || !hasHookEntry(data, ev) {
			t.Errorf("%s: want one gochecks entry, got %v", ev, s.Hooks[ev])
		}
	}
	if len(s.Permissions.Deny) != 1 || s.Permissions.Deny[0] != allowDeny {
		t.Errorf("deny: %v", s.Permissions.Deny)
	}
}

// Without git the Stop check sees nothing; that is reported, not a pass.
func TestStopWithoutGitIsReported(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if r := runHookEvent(t, stopEvent("g1", dir, false)); r.code != 1 || !strings.Contains(r.stderr, "git not on PATH") {
		t.Errorf("want exit 1 naming git: %+v", r)
	}
}

// A file that does not parse is one blocking error, not a failed run:
// before, the formatter's failure made check exit with an error, and
// the hooks passed that on as a non-blocking exit 1.
func TestSyntaxErrorBlocks(t *testing.T) {
	hasGo(t)
	isolateHome(t)
	dir := gitRepo(t, stopFixture())
	bad := "package fixture\n\nfunc A() int {\n\tx := \"unterminated\n\treturn 1\n}\n"
	edit(t, filepath.Join(dir, "a.go"), bad)
	a := filepath.Join(dir, "a.go")

	for _, noApply := range []bool{true, false} {
		var code int
		out := captureStdout(t, func() { code = runCheckWith([]string{a}, noApply, false, defaultConfig()) })
		fs := parseJSONL(t, out)
		if code != 2 || len(fs) == 0 || fs[0].Line != 4 || fs[0].Class != "error" {
			t.Errorf("no-apply=%t: exit %d, findings:\n%s", noApply, code, out)
		}
		at := map[int]int{}
		for _, f := range fs {
			at[f.Line*1000+f.Col]++
		}
		for p, n := range at {
			if n > 1 {
				t.Errorf("no-apply=%t: %d findings at %d:%d:\n%s", noApply, n, p/1000, p%1000, out)
			}
		}
	}
	if r := runHookEvent(t, hookEvent(dir, a)); r.code != 2 || !strings.Contains(r.stderr, "a.go:4:") {
		t.Errorf("PostToolUse: %+v", r)
	}
	if r := runHookEvent(t, stopEvent("x1", dir, false)); r.code != 2 || !strings.Contains(r.stderr, "a.go:4:") {
		t.Errorf("Stop: %+v", r)
	}
}
