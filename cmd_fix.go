package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func runFix(args []string) int {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		changedFlag string
		textOut     bool
	)
	fs.StringVar(&changedFlag, "changed", "", "whitespace- or comma-separated list of changed files")
	fs.BoolVar(&textOut, "text", false, "human-readable text output (default: JSON lines)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	raw := splitList(changedFlag)
	raw = append(raw, fs.Args()...)
	if len(raw) == 0 {
		fmt.Fprintln(os.Stderr, "gochecks fix: no files given (see gochecks help)")
		return 2
	}

	files, err := resolveChanged(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks:", err)
		return 2
	}
	code := 0
	if len(files) > 0 {
		cfg, err := loadConfig(filepath.Dir(files[0]))
		if err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: config:", err)
			return 2
		}
		if files = cfg.applyExclude(files); len(files) > 0 {
			code = fixFiles(files, textOut, cfg)
		}
	}
	return missingExit("fix", raw, code)
}

// fixFiles formats and modernizes files, then checks them.
func fixFiles(files []string, textOut bool, cfg Config) int {
	// Parse errors come back from the check below.
	if _, err := applyFormat(files); err != nil {
		fmt.Fprintln(os.Stderr, "gochecks:", err)
		return 2
	}

	// Modernize first, even if warnings remain; the check pass then
	// reports what go fix could not apply.
	scope := make(map[string]bool, len(files))
	for _, f := range files {
		scope[f] = true
	}
	for pkgDir := range groupByPackage(files) {
		if err := modernizeAndFormat(pkgDir, scope); err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: modernize:", err)
			return 2
		}
	}
	fixCfg := cfg
	fixCfg.AutoFix = true
	return runCheckWith(files, false, textOut, fixCfg)
}
