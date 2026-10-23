package main

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleWorkflow = `name: ci
jobs:
  test:
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - uses: golangci/golangci-lint-action@v8
        with:
          version: v2.12.2
      - uses: ./.github/actions/local
      - uses: some/action@0123456789abcdef0123456789abcdef01234567
      - run: go install honnef.co/go/tools/cmd/staticcheck@v0.6.1
      - uses: goreleaser/goreleaser-action@v6
`

func TestParseWorkflowPins(t *testing.T) {
	pins := parseWorkflowPins("ci.yml", sampleWorkflow)
	want := map[string]string{
		"action actions/checkout":                 "v5",
		"action actions/setup-go":                 "v5",
		"action golangci/golangci-lint-action":    "v8",
		"lint golangci-lint":                      "v2.12.2",
		"tool honnef.co/go/tools/cmd/staticcheck": "v0.6.1",
		"action goreleaser/goreleaser-action":     "v6",
	}
	got := map[string]string{}
	for _, p := range pins {
		got[p.kind+" "+p.name] = p.version
	}
	if len(got) != len(want) {
		t.Fatalf("pins: got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
}

func TestVersionBehind(t *testing.T) {
	cases := []struct {
		pinned, latest string
		behind         bool
	}{
		{"v5", "v6.1.0", true},
		{"v6", "v6.1.0", false},
		{"v2.12.2", "v2.15.0", true},
		{"v2.15.0", "v2.15.0", false},
		{"v6", "v5.0.0", false},
		{"main", "v1.0.0", false},
		{"v1.2.3", "v1.2.3-rc1", false},
	}
	for _, c := range cases {
		if got := versionBehind(c.pinned, c.latest); got != c.behind {
			t.Errorf("%s vs %s: got %v", c.pinned, c.latest, got)
		}
	}
}

func TestWorkflowsBehind(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ci.yml"), []byte(sampleWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	latestAction := func(name string) (string, error) {
		return map[string]string{
			"actions/checkout":              "v5.0.1",
			"actions/setup-go":              "v6.1.0",
			"golangci/golangci-lint-action": "v8.0.0",
			"goreleaser/goreleaser-action":  "v6.4.0",
		}[name], nil
	}
	latestModule := func(m string) string {
		return map[string]string{
			"github.com/golangci/golangci-lint/v2": "v2.15.0",
			"honnef.co/go/tools":                   "v0.8.2",
		}[m]
	}
	deps, err := workflowsBehind(root, latestAction, latestModule)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range deps {
		got[d.Name] = d.Current + "->" + d.Latest
	}
	want := map[string]string{
		"actions/setup-go (ci.yml)":                   "v5->v6.1.0",
		"golangci-lint (ci.yml)":                      "v2.12.2->v2.15.0",
		"honnef.co/go/tools/cmd/staticcheck (ci.yml)": "v0.6.1->v0.8.2",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
}
