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
	fs.StringVar(&changedFlag, "changed", "", "whitespace- or comma-separated files the agent touched")
	fs.BoolVar(&textOut, "text", false, "human-readable text output (default: JSON lines)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	raw := splitList(changedFlag)
	raw = append(raw, fs.Args()...)

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

	pkgs := groupByPackage(files)
	var extra []Finding
	for pkgDir := range pkgs {
		if err := runModernize(pkgDir); err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: modernize:", err)
			return 2
		}
		extra = append(extra, modernizeConflicts(pkgDir)...)
	}

	// `gochecks fix` implies silent auto-apply for every tool that
	// supports it — force AutoFix on for the subsequent check pass
	// regardless of the user's config default.
	fixCfg := cfg
	fixCfg.AutoFix = true
	code := runCheckWith(files, false, textOut, fixCfg)
	if len(extra) > 0 {
		emit(extra, textOut)
		invocationFindings += len(extra)
	}
	return code
}
