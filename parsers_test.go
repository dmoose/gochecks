package main

import (
	"path/filepath"
	"testing"
)

func TestParseBuild(t *testing.T) {
	out := `# example.com/pkg
./main.go:4:17: cannot use 42 (untyped int constant) as string value in variable declaration
./main.go:5:9: cannot use x (variable of type string) as int value in return statement
`
	fs := parseBuild(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(fs), fs)
	}
	f := fs[0]
	if f.Class != "error" || f.Tool != "build" {
		t.Errorf("bad class/tool: %+v", f)
	}
	if f.Line != 4 || f.Col != 17 {
		t.Errorf("bad line/col: %+v", f)
	}
	if filepath.Base(f.File) != "main.go" {
		t.Errorf("bad file: %s", f.File)
	}
}

func TestParseBuildNoMatch(t *testing.T) {
	out := `# pkg
random garbage
package not found
`
	if fs := parseBuild(out, "/tmp"); len(fs) != 0 {
		t.Errorf("expected 0 findings on garbage input, got %d: %+v", len(fs), fs)
	}
}

func TestParseBuildEvents(t *testing.T) {
	out := `{"ImportPath":"x","Action":"build-output","Output":"# x\n"}
{"ImportPath":"x","Action":"build-output","Output":"./main.go:3:26: declared and not used: y\n"}
{"ImportPath":"x","Action":"build-fail"}
`
	fs := parseBuildEvents(out, "/tmp/pkg")
	if len(fs) != 1 || fs[0].Line != 3 || fs[0].Col != 26 || fs[0].File != "/tmp/pkg/main.go" {
		t.Fatalf("got %+v", fs)
	}
}

func TestParseVet(t *testing.T) {
	out := `{
	"example.com/pkg": {
		"printf": [
			{"posn": "/tmp/pkg/main.go:42:9", "end": "/tmp/pkg/main.go:42:11", "message": "fmt.Printf format %d has arg \"s\" of wrong type string"}
		],
		"unreachable": [
			{"posn": "main.go:77:2", "message": "unreachable code", "suggested_fixes": []}
		]
	}
}
`
	fs := parseVet(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(fs), fs)
	}
	if fs[0].Class != "error" || fs[0].Tool != "vet" || fs[0].Line != 42 || fs[0].Col != 9 {
		t.Errorf("bad finding: %+v", fs[0])
	}
	if fs[1].File != "/tmp/pkg/main.go" || fs[1].Line != 77 {
		t.Errorf("relative posn not anchored: %+v", fs[1])
	}
	if fs := parseVet("{}\n", "/tmp"); len(fs) != 0 {
		t.Errorf("empty tree should yield no findings: %+v", fs)
	}
}

func TestParseVetText(t *testing.T) {
	out := "# x\nvet: ./b.go:3:12: declared and not used: y\n"
	fs := parseVetText(out, "/tmp/pkg")
	if len(fs) != 1 || fs[0].File != "/tmp/pkg/b.go" || fs[0].Line != 3 || fs[0].Class != "error" {
		t.Fatalf("got %+v", fs)
	}
}

func TestParsePosn(t *testing.T) {
	cases := []struct {
		in   string
		file string
		line int
		col  int
	}{
		{"/a/b.go:3:4", "/a/b.go", 3, 4},
		{"b.go:7", "b.go", 7, 0},
		{"C:/x/y.go:1:2", "C:/x/y.go", 1, 2},
		{"-", "-", 0, 0},
	}
	for _, c := range cases {
		f, l, col := parsePosn(c.in)
		if f != c.file || l != c.line || col != c.col {
			t.Errorf("%q: got %q %d %d", c.in, f, l, col)
		}
	}
}

func TestParseStaticcheck(t *testing.T) {
	out := `{"code":"SA4006","severity":"error","location":{"file":"/tmp/pkg/a.go","line":6,"column":2},"end":{"file":"/tmp/pkg/a.go","line":6,"column":27},"message":"this value of x is never used"}
{"code":"U1000","severity":"warning","location":{"file":"main.go","line":25,"column":6},"end":{"file":"main.go","line":25,"column":9},"message":"func Foo is unused"}
{"code":"ST1000","severity":"ignored","location":{"file":"main.go","line":1,"column":1},"end":{"file":"main.go","line":1,"column":1},"message":"at least one file in a package should have a package comment"}
`
	fs := parseStaticcheck(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings (ignored dropped), got %d: %+v", len(fs), fs)
	}
	for _, f := range fs {
		if f.Class != "warning" || f.Tool != "staticcheck" {
			t.Errorf("bad class/tool: %+v", f)
		}
	}
	if fs[0].Msg != "this value of x is never used (SA4006)" || fs[0].Line != 6 {
		t.Errorf("message should carry the check code: %+v", fs[0])
	}
	if fs[1].File != "/tmp/pkg/main.go" {
		t.Errorf("relative file not anchored: %+v", fs[1])
	}
}

func TestParseModernize(t *testing.T) {
	out := `{"x": {"slicescontains": [{"posn": "/tmp/pkg/a.go:15:2", "end": "/tmp/pkg/a.go:19:3", "message": "Loop can be simplified using slices.Contains", "suggested_fixes": [{"message": "Replace loop", "edits": []}]}]}}`
	fs := parseModernize(out, "/tmp/pkg")
	if len(fs) != 1 || fs[0].Class != "suggestion" || fs[0].Tool != "modernize" || fs[0].Line != 15 {
		t.Fatalf("got %+v", fs)
	}
	if fs[0].Msg != "Loop can be simplified using slices.Contains (slicescontains; conflicts with another fix, apply by hand)" {
		t.Errorf("msg: %q", fs[0].Msg)
	}
	if fs := parseModernize("", "/tmp"); len(fs) != 0 {
		t.Errorf("no output should yield no findings")
	}
}

func TestParseErrcheck(t *testing.T) {
	// Tab separator between col and source is errcheck's signature.
	out := "main.go:8:11:\tos.Remove(\"bar\")\n" +
		"main.go:14:5:\tf.Close()\n"
	fs := parseErrcheck(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(fs), fs)
	}
	if fs[0].Class != "warning" || fs[0].Tool != "errcheck" {
		t.Errorf("bad class/tool: %+v", fs[0])
	}
	if fs[0].Line != 8 || fs[0].Col != 11 {
		t.Errorf("bad line/col: %+v", fs[0])
	}
	if fs[0].Msg == "" || fs[1].Msg == "" {
		t.Errorf("expected Msg to include source context: %+v", fs)
	}
}

func TestParseIneffassign(t *testing.T) {
	out := `/tmp/pkg/main.go:9:2: ineffectual assignment to x
/tmp/pkg/main.go:21:5: ineffectual assignment to result
`
	fs := parseIneffassign(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(fs), fs)
	}
	if fs[0].Class != "warning" || fs[0].Tool != "ineffassign" {
		t.Errorf("bad class/tool: %+v", fs[0])
	}
	if fs[0].Line != 9 || fs[0].Col != 2 {
		t.Errorf("bad line/col: %+v", fs[0])
	}
}

func TestParseGolangciLint(t *testing.T) {
	out := `{"Issues":[
		{"FromLinter":"errcheck","Text":"Error return value of ` + "`" + `os.Remove` + "`" + ` is not checked","Severity":"","Pos":{"Filename":"main.go","Line":8,"Column":11}},
		{"FromLinter":"gosec","Text":"G304","Severity":"error","Pos":{"Filename":"main.go","Line":20,"Column":3}}
	]}`
	fs := parseGolangciLint(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(fs), fs)
	}
	if fs[0].Tool != "golangci-lint/errcheck" {
		t.Errorf("bad tool tag: %+v", fs[0])
	}
	if fs[0].Class != "warning" {
		t.Errorf("empty severity should map to warning: %+v", fs[0])
	}
	if fs[1].Class != "error" {
		t.Errorf("severity=error should map to error: %+v", fs[1])
	}
	if filepath.Base(fs[0].File) != "main.go" {
		t.Errorf("bad file: %s", fs[0].File)
	}
}

func TestParseGolangciLintTrailingText(t *testing.T) {
	out := `{"Issues":[{"FromLinter":"gosec","Text":"G204","Severity":"","Pos":{"Filename":"a.go","Line":1,"Column":1}}]}
2 issues:
* gosec: 2
`
	fs := parseGolangciLint(out, "/tmp/pkg")
	if len(fs) != 1 {
		t.Fatalf("report followed by stats text: got %d findings, want 1", len(fs))
	}
	if fs[0].Class != "warning" {
		t.Errorf("gosec should be a warning: %+v", fs[0])
	}
	if fs := parseGolangciLint(`{"Issues":[{"FromLinter":"perfsprint","Text":"x","Pos":{"Filename":"a.go","Line":1}}]}`, "/p"); fs[0].Class != "suggestion" {
		t.Errorf("perfsprint should be a suggestion: %+v", fs[0])
	}
}

func TestParseGolangciLintEmpty(t *testing.T) {
	if fs := parseGolangciLint("", "/tmp"); len(fs) != 0 {
		t.Errorf("empty input should yield 0 findings, got %d", len(fs))
	}
	if fs := parseGolangciLint("not json", "/tmp"); len(fs) != 0 {
		t.Errorf("garbage input should yield 0 findings, got %d", len(fs))
	}
}

func TestParseTestEvents(t *testing.T) {
	// A mini test2json stream: one pass, one fail with a file:line output.
	out := `{"Action":"run","Package":"x","Test":"TestA"}
{"Action":"output","Package":"x","Test":"TestA","Output":"=== RUN   TestA\n"}
{"Action":"pass","Package":"x","Test":"TestA","Elapsed":0.01}
{"Action":"run","Package":"x","Test":"TestB"}
{"Action":"output","Package":"x","Test":"TestB","Output":"    thing_test.go:17: got 1, want 2\n"}
{"Action":"fail","Package":"x","Test":"TestB","Elapsed":0.02}
`
	fs := parseTestEvents(out, "/tmp/pkg")
	if len(fs) != 1 {
		t.Fatalf("expected 1 failure, got %d: %+v", len(fs), fs)
	}
	f := fs[0]
	if f.Class != "error" || f.Tool != "test" {
		t.Errorf("bad class/tool: %+v", f)
	}
	if f.Line != 17 {
		t.Errorf("bad line: %+v", f)
	}
	if filepath.Base(f.File) != "thing_test.go" {
		t.Errorf("bad file: %s", f.File)
	}
}

func TestParseTestEventsNoFailureLine(t *testing.T) {
	// A failure without a file:line output still yields a finding.
	out := `{"Action":"run","Package":"x","Test":"TestPanic"}
{"Action":"output","Package":"x","Test":"TestPanic","Output":"panic: runtime error\n"}
{"Action":"fail","Package":"x","Test":"TestPanic"}
`
	fs := parseTestEvents(out, "/tmp/pkg")
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(fs))
	}
	if fs[0].Msg == "" {
		t.Error("expected non-empty Msg on fallback finding")
	}
}
