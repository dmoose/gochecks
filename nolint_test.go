package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// Under the embedded profile a suppression must name its linter and
// say why: a bare //nolint, or one without a reason, is a warning. One
// with both passes.
func TestNolintNeedsLinterAndReason(t *testing.T) {
	hasGo(t)
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("golangci-lint not on PATH")
	}
	isolateHome(t)
	dir := writeFixture(t, map[string]string{
		"go.mod": "module fixture\n\ngo 1.27\n",
		"a.go": `package fixture

func fail() error { return nil }

func Bare() {
	fail() //nolint
}

func NoReason() {
	fail() //nolint:errcheck
}

func Explained() {
	fail() //nolint:errcheck // fail never fails here
}
`,
	})

	var code int
	out := captureStdout(t, func() { code = runCheck([]string{"--no-apply", filepath.Join(dir, "a.go")}) })
	var got []string
	for _, f := range parseJSONL(t, out) {
		got = append(got, fmt.Sprintf("%d %s/%s: %s", f.Line, f.Class, f.Tool, f.Msg))
	}
	slices.Sort(got)
	want := []string{
		"10 warning/golangci-lint/nolintlint: directive `//nolint:errcheck` should provide explanation such as `//nolint:errcheck // this is why`",
		"6 warning/golangci-lint/nolintlint: directive `//nolint` should mention specific linter such as `//nolint:my-linter`",
	}
	if code != 1 || !slices.Equal(got, want) {
		t.Errorf("exit %d; findings %q, want %q\n%s", code, got, want, out)
	}
}
