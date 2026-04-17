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

func TestParseVet(t *testing.T) {
	out := `# example.com/pkg
./main.go:42:9: Printf format %d has arg of type string
./main.go:77:2: unreachable code
`
	fs := parseVet(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(fs), fs)
	}
	if fs[0].Class != "error" || fs[0].Tool != "vet" {
		t.Errorf("bad class/tool: %+v", fs[0])
	}
}

func TestParseStaticcheck(t *testing.T) {
	out := `./main.go:10:2: should check returned error before deferring f.Close() (SA5001)
./main.go:25:6: func Foo is unused (U1000)
`
	fs := parseStaticcheck(out, "/tmp/pkg")
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(fs), fs)
	}
	for _, f := range fs {
		if f.Class != "warning" || f.Tool != "staticcheck" {
			t.Errorf("bad class/tool: %+v", f)
		}
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
	// Test panics or fails without a "file.go:line:" output — still emit a
	// generic finding so the signal isn't lost.
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
