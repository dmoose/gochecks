package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const configFilename = ".gochecks.json"

// Config is the per-repo gochecks configuration. Every field is
// optional; missing fields fall back to defaults. Keep this struct
// ruthlessly minimal — each field is a compatibility commitment.
type Config struct {
	// Test enables `go test` on changed packages during check.
	// Off by default because test suites vary wildly in cost and
	// side-effect profile.
	Test bool `json:"test"`

	// TestTimeout is passed to `go test -timeout`. Must parse as a
	// Go duration. Defaults to 30s.
	TestTimeout string `json:"test_timeout"`

	// Exclude is a list of path globs (doublestar style) that are
	// filtered out of --changed before any tool runs. Useful for
	// repos with generated subtrees the default detector misses.
	Exclude []string `json:"exclude"`

	// AutoFix extends silent apply from formatting to modernize.
	// When true and check produces no errors or warnings, gochecks
	// runs `go fix` modernize on the changed packages, emitting
	// only the conflicts it couldn't auto-apply. When Linters is
	// also true, AutoFix additionally enables golangci-lint's
	// `--fix` pass for its autofix-capable diagnostics.
	AutoFix bool `json:"auto_fix"`

	// Linters enables the optional golangci-lint umbrella pass
	// during check. Off by default because golangci-lint adds
	// startup latency; users opt in when they have a configured
	// `.golangci.yml` in their repo. When enabled, gochecks does
	// NOT run errcheck/ineffassign standalone — golangci-lint
	// owns that coverage to avoid double-reporting. Users should
	// also disable `modernize` in their `.golangci.yml` since Go
	// 1.26's `go fix` (always on) already covers it.
	Linters bool `json:"linters"`

	// Path is set after load to the config file that was read, or
	// empty if defaults were used.
	Path string `json:"-"`
}

func defaultConfig() Config {
	return Config{
		Test:        false,
		TestTimeout: "30s",
	}
}

// loadConfig walks up from startDir looking for .gochecks.json, stopping
// at the filesystem root. Returns defaults if nothing is found.
// Errors in an existing config file are surfaced — silent fallback
// would mask typos and waste debugging cycles.
func loadConfig(startDir string) (Config, error) {
	cfg := defaultConfig()
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return cfg, err
	}
	for {
		path := filepath.Join(dir, configFilename)
		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, &cfg); err != nil {
				return cfg, fmt.Errorf("%s: %w", path, err)
			}
			cfg.Path = path
			return cfg, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cfg, nil
		}
		dir = parent
	}
}

func (c Config) testTimeout() time.Duration {
	d, err := time.ParseDuration(c.TestTimeout)
	if err != nil || d <= 0 {
		return 30 * time.Second
	}
	return d
}

// applyExclude filters files whose absolute path matches any exclude
// pattern. A pattern containing "/" is matched as a path substring;
// otherwise it's matched against the file's basename via
// filepath.Match (so "*_gen.go", "mock_*.go", etc. work).
func (c Config) applyExclude(files []string) []string {
	if len(c.Exclude) == 0 {
		return files
	}
	out := files[:0:0]
	for _, f := range files {
		if !c.excluded(f) {
			out = append(out, f)
		}
	}
	return out
}

func (c Config) excluded(path string) bool {
	base := filepath.Base(path)
	for _, pat := range c.Exclude {
		if strings.Contains(pat, "/") {
			if strings.Contains(path, pat) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
	}
	return false
}
