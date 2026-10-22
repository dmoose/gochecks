package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	c := defaultConfig()
	if c.Test {
		t.Error("default Test should be false")
	}
	if c.testTimeout() != 30*time.Second {
		t.Errorf("default testTimeout = %v, want 30s", c.testTimeout())
	}
}

func TestTestTimeoutFallback(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 30 * time.Second},
		{"bogus", 30 * time.Second},
		{"0s", 30 * time.Second},
		{"-1s", 30 * time.Second},
		{"5s", 5 * time.Second},
		{"2m", 2 * time.Minute},
	}
	for _, tc := range cases {
		c := Config{TestTimeout: tc.in}
		if got := c.testTimeout(); got != tc.want {
			t.Errorf("testTimeout(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestExcluded(t *testing.T) {
	c := Config{Exclude: []string{"*_gen.go", "testdata/", "mock_*.go"}}
	cases := []struct {
		path string
		want bool
	}{
		{"/repo/foo.go", false},
		{"/repo/foo_gen.go", true},
		{"/repo/pkg/bar_gen.go", true},
		{"/repo/testdata/fixture.go", true},
		{"/repo/pkg/testdata/x.go", true},
		{"/repo/mock_thing.go", true},
		{"/repo/pkg/not_matching.go", false},
	}
	for _, tc := range cases {
		if got := c.excluded(tc.path); got != tc.want {
			t.Errorf("excluded(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestApplyExcludeEmpty(t *testing.T) {
	c := Config{}
	in := []string{"a.go", "b.go"}
	out := c.applyExclude(in)
	if len(out) != 2 {
		t.Errorf("empty Exclude should pass all files; got %v", out)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	dir := t.TempDir()
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig unexpectedly failed: %v", err)
	}
	if c.Path != "" {
		t.Errorf("missing config should yield empty Path, got %q", c.Path)
	}
	if c.Test {
		t.Error("missing config should default Test=false")
	}
}

func TestLoadConfigWalksUp(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, configFilename)
	if err := os.WriteFile(cfgPath, []byte(`{"test": true, "test_timeout": "90s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(nested)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !c.Test {
		t.Error("expected Test=true from parent config")
	}
	if c.testTimeout() != 90*time.Second {
		t.Errorf("testTimeout = %v, want 90s", c.testTimeout())
	}
	if c.Path != cfgPath {
		t.Errorf("Path = %q, want %q", c.Path, cfgPath)
	}
}

func TestLoadConfigMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configFilename), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(dir)
	if err == nil {
		t.Error("expected error on malformed config, got nil")
	}
}
