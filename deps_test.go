package main

import (
	"strings"
	"testing"
)

func TestParseModulesBehind(t *testing.T) {
	out := `{"Path":"git.6-p.cc/catapulsion/resender","Main":true,"Version":""}
{"Path":"github.com/a-h/templ","Version":"v0.3.1070","Update":{"Version":"v0.3.1100"}}
{"Path":"git.6-p.cc/catapulsion/plinth","Version":"v0.2.0","Update":{"Version":"v0.4.0"}}
{"Path":"cel.dev/cel-go","Version":"v0.32.0"}
{"Path":"golang.org/x/text","Version":"v0.41.0","Indirect":true,"Update":{"Version":"v0.42.0"}}
`
	deps := parseModulesBehind(out)
	if len(deps) != 2 {
		t.Fatalf("want 2 behind (indirect and current skipped), got %+v", deps)
	}
	if deps[0].Name != "git.6-p.cc/catapulsion/plinth" || !deps[0].Own {
		t.Errorf("own module should come first: %+v", deps)
	}
	if deps[1].Name != "github.com/a-h/templ" || deps[1].Own || deps[1].Latest != "v0.3.1100" {
		t.Errorf("third-party module: %+v", deps[1])
	}
	if got := deps[0].text(); got != "module git.6-p.cc/catapulsion/plinth v0.2.0 -> v0.4.0 (own)" {
		t.Errorf("text: %q", got)
	}
	if ownerPrefix("example.com") != "" || ownerPrefix("github.com/dmoose/gochecks") != "github.com/dmoose/" {
		t.Error("ownerPrefix")
	}
}

func TestSniffVersion(t *testing.T) {
	cases := map[string]string{
		"/*! htmx.org 2.0.11 */ var htmx=...":          "2.0.11",
		"/*! ECharts v5.5.1 (c) 2024 */":               "5.5.1",
		"var x = {version:\"3.4.5\", other:\"9.9.9\"}": "3.4.5",
		"const VERSION = '1.2.3';":                     "1.2.3",
		"no version here":                              "",
	}
	for in, want := range cases {
		if got := sniffVersion([]byte(in)); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDepsSummary(t *testing.T) {
	if depsSummary(nil) != "" {
		t.Error("no deps, no summary")
	}
	s := depsSummary([]dep{{Kind: "module", Name: "git.6-p.cc/catapulsion/plinth", Current: "v0.2.0", Latest: "v0.4.0"}, {Kind: "asset", Name: "static/htmx.min.js", Current: "2.0.4", Latest: "2.0.11"}})
	if !strings.HasPrefix(s, "gochecks: 2 dependencies behind: plinth v0.2.0 -> v0.4.0, htmx.min.js 2.0.4 -> 2.0.11") {
		t.Errorf("summary: %q", s)
	}
}
