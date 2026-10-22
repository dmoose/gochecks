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
	if len(files) == 0 {
		return 0
	}
	cfg, err := loadConfig(filepath.Dir(files[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks: config:", err)
		return 2
	}
	files = cfg.applyExclude(files)
	if len(files) == 0 {
		return 0
	}

	if err := applyFormat(files); err != nil {
		fmt.Fprintln(os.Stderr, "gochecks:", err)
		return 2
	}

	// Modernize first, even if warnings remain; the check pass then
	// reports what go fix could not apply.
	for pkgDir := range groupByPackage(files) {
		if err := runModernize(pkgDir); err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: modernize:", err)
			return 2
		}
	}
	fixCfg := cfg
	fixCfg.AutoFix = true
	return runCheckWith(files, false, textOut, fixCfg)
}
