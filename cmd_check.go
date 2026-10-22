package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		changedFlag string
		noApply     bool
		textOut     bool
	)
	fs.StringVar(&changedFlag, "changed", "", "whitespace- or comma-separated list of changed files")
	fs.BoolVar(&noApply, "no-apply", false, "report formatting instead of fixing it")
	fs.BoolVar(&textOut, "text", false, "human-readable text output (default: JSON lines)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	raw := splitList(changedFlag)
	raw = append(raw, fs.Args()...)
	if len(raw) == 0 {
		fmt.Fprintln(os.Stderr, "gochecks check: no files given (see gochecks help)")
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
	return runCheckWith(files, noApply, textOut, cfg)
}

func runCheckWith(files []string, noApply, textOut bool, cfg Config) int {
	findings, err := check(files, noApply, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks:", err)
		return 2
	}
	invocationFindings = len(findings)
	emit(findings, textOut)
	return exitFor(findings)
}

// check formats (or reports formatting for) files, then runs the
// per-package analyzers and returns the scoped findings.
func check(files []string, noApply bool, cfg Config) ([]Finding, error) {
	var findings []Finding

	if noApply {
		fmtFindings, err := runGofmt(files, false)
		if err != nil {
			return nil, fmt.Errorf("gofmt: %w", err)
		}
		findings = append(findings, fmtFindings...)

		impFindings, err := runGoimports(files, false)
		switch {
		case err == errGoimportsMissing:
			fmt.Fprintln(os.Stderr, goimportsMissingNote)
		case err != nil:
			return nil, fmt.Errorf("goimports: %w", err)
		default:
			findings = append(findings, impFindings...)
		}
	} else if err := applyFormat(files); err != nil {
		return nil, err
	}

	scope := make(map[string]bool, len(files))
	for _, f := range files {
		scope[f] = true
	}

	pkgs := groupByPackage(files)
	pkgFindings, err := analyzePackages(pkgs, scope, cfg)
	if err != nil {
		return nil, err
	}
	findings = append(findings, pkgFindings...)

	// Modernize only a tree with nothing blocking in it.
	if cfg.AutoFix && !hasBlockingFinding(findings) {
		for pkgDir := range pkgs {
			if err := runModernize(pkgDir); err != nil {
				return nil, fmt.Errorf("auto_fix modernize: %w", err)
			}
			findings = append(findings, modernizeConflicts(pkgDir)...)
		}
	}
	return findings, nil
}

type pkgResult struct {
	findings []Finding
	err      error
}

// analyzePackages runs the analyzers per package, NumCPU at a time.
func analyzePackages(pkgs map[string][]string, scope map[string]bool, cfg Config) ([]Finding, error) {
	probes := toolProbes{
		staticcheck: staticcheckAvailable(),
		errcheck:    errcheckAvailable(),
		ineffassign: ineffassignAvailable(),
		golangci:    cfg.Linters && golangciLintAvailable(),
	}
	// golangci-lint runs errcheck and ineffassign itself.
	if probes.golangci {
		probes.errcheck = false
		probes.ineffassign = false
	}
	results := make(chan pkgResult, len(pkgs))

	workers := max(min(runtime.NumCPU(), len(pkgs)), 1)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for pkgDir := range pkgs {
		wg.Add(1)
		sem <- struct{}{}
		go func(pkgDir string) {
			defer wg.Done()
			defer func() { <-sem }()
			results <- analyzeOne(pkgDir, scope, cfg, probes)
		}(pkgDir)
	}
	wg.Wait()
	close(results)

	var findings []Finding
	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		findings = append(findings, r.findings...)
	}
	return findings, nil
}

type toolProbes struct {
	staticcheck, errcheck, ineffassign, golangci bool
}

func analyzeOne(pkgDir string, scope map[string]bool, cfg Config, p toolProbes) pkgResult {
	var r pkgResult
	build, err := runBuild(pkgDir)
	if err != nil {
		r.err = fmt.Errorf("build: %w", err)
		return r
	}
	r.findings = filterScope(build, scope)
	// Analyzers on a package that does not compile only repeat the
	// type errors.
	if len(build) > 0 {
		return r
	}
	for _, a := range analyzers {
		if !a.enabled(cfg, p) {
			continue
		}
		fs, err := a.run(pkgDir, cfg)
		if err != nil {
			r.err = fmt.Errorf("%s: %w", a.name, err)
			return r
		}
		if !a.unscoped {
			fs = filterScope(fs, scope)
		}
		r.findings = append(r.findings, fs...)
	}
	return r
}

func hasBlockingFinding(findings []Finding) bool {
	for _, f := range findings {
		if f.Class == "error" || f.Class == "warning" {
			return true
		}
	}
	return false
}

func applyFormat(files []string) error {
	if _, err := runGofmt(files, true); err != nil {
		return fmt.Errorf("gofmt: %w", err)
	}
	_, err := runGoimports(files, true)
	if err == errGoimportsMissing {
		fmt.Fprintln(os.Stderr, goimportsMissingNote)
		return nil
	}
	if err != nil {
		return fmt.Errorf("goimports: %w", err)
	}
	return nil
}

func exitFor(findings []Finding) int {
	code := 0
	for _, f := range findings {
		switch f.Class {
		case "error":
			if code < 2 {
				code = 2
			}
		case "warning":
			if code < 1 {
				code = 1
			}
		}
	}
	return code
}

const goimportsMissingNote = "gochecks: goimports not installed; skipping (go install golang.org/x/tools/cmd/goimports@latest)"

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}
