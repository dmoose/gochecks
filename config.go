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

// Config is the per-repo .gochecks.json. Every field is optional.
type Config struct {
	// Test runs go test on changed packages.
	Test bool `json:"test"`

	// TestTimeout is passed to go test -timeout. Defaults to 30s.
	TestTimeout string `json:"test_timeout"`

	// Exclude lists patterns dropped from the changed files before any
	// tool runs. A pattern with a "/" matches as a path substring,
	// otherwise against the basename.
	Exclude []string `json:"exclude"`

	// AutoFix also applies go fix modernize when check finds nothing
	// blocking, and adds --fix to golangci-lint when Linters is on.
	AutoFix bool `json:"auto_fix"`

	// Linters controls golangci-lint, which runs whenever it is on PATH
	// unless this is false. The repo's .golangci.yml is used when there
	// is one, otherwise the profile embedded in gochecks. staticcheck,
	// errcheck and ineffassign are left to it when it runs.
	Linters *bool `json:"linters,omitempty"`

	// Path is the config file read, or "" when defaults were used.
	Path string `json:"-"`
}

func defaultConfig() Config {
	return Config{
		Test:        false,
		TestTimeout: "30s",
	}
}

// loadConfig walks up from startDir to the root looking for
// .gochecks.json and returns defaults when there is none. A config
// file that fails to parse is an error, not a fallback.
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

// lintersOn reports whether golangci-lint may run.
func (c Config) lintersOn() bool { return c.Linters == nil || *c.Linters }

func (c Config) testTimeout() time.Duration {
	d, err := time.ParseDuration(c.TestTimeout)
	if err != nil || d <= 0 {
		return 30 * time.Second
	}
	return d
}

// applyExclude drops files matching any Exclude pattern.
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
