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
	fs.StringVar(&changedFlag, "changed", "", "whitespace- or comma-separated files the agent touched")
	fs.BoolVar(&noApply, "no-apply", false, "report formatting instead of fixing it")
	fs.BoolVar(&textOut, "text", false, "human-readable text output (default: JSON lines)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	raw := splitList(changedFlag)
	raw = append(raw, fs.Args()...)

	files, err := resolveChanged(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gocheck:", err)
		return 2
	}
	if len(files) == 0 {
		return 0
	}
	cfg, err := loadConfig(filepath.Dir(files[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gocheck: config:", err)
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
		fmt.Fprintln(os.Stderr, "gocheck:", err)
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

	// AutoFix: extend the silent-apply path from gofmt/goimports to
	// modernize. Only fires when nothing above is blocking, so we
	// never silently mutate code that already has real issues.
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

// analyzePackages fans out build/vet/staticcheck/test across packages
// with a worker count capped at NumCPU. Returns the first error it
// sees so the caller can bail cleanly.
func analyzePackages(pkgs map[string][]string, scope map[string]bool, cfg Config) ([]Finding, error) {
	probes := toolProbes{
		staticcheck: staticcheckAvailable(),
		errcheck:    errcheckAvailable(),
		ineffassign: ineffassignAvailable(),
		golangci:    cfg.Linters && golangciLintAvailable(),
	}
	// The umbrella owns errcheck/ineffassign to avoid double-reporting.
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
	r.findings = append(r.findings, filterScope(build, scope)...)
	// A package that does not compile has nothing further to say: every
	// analyzer would only echo the same type errors back.
	if len(build) > 0 {
		return r
	}

	vet, err := runVet(pkgDir)
	if err != nil {
		r.err = fmt.Errorf("vet: %w", err)
		return r
	}
	r.findings = append(r.findings, filterScope(vet, scope)...)

	if p.staticcheck {
		scf, err := runStaticcheck(pkgDir)
		if err != nil {
			r.err = fmt.Errorf("staticcheck: %w", err)
			return r
		}
		r.findings = append(r.findings, filterScope(scf, scope)...)
	}

	if p.errcheck {
		ef, err := runErrcheck(pkgDir)
		if err != nil {
			r.err = fmt.Errorf("errcheck: %w", err)
			return r
		}
		r.findings = append(r.findings, filterScope(ef, scope)...)
	}

	if p.ineffassign {
		inf, err := runIneffassign(pkgDir)
		if err != nil {
			r.err = fmt.Errorf("ineffassign: %w", err)
			return r
		}
		r.findings = append(r.findings, filterScope(inf, scope)...)
	}

	if p.golangci {
		gl, err := runGolangciLint(pkgDir, cfg.AutoFix)
		if err != nil {
			r.err = fmt.Errorf("golangci-lint: %w", err)
			return r
		}
		r.findings = append(r.findings, filterScope(gl, scope)...)
	}

	if cfg.Test {
		tests, err := runGoTest(pkgDir, cfg.testTimeout())
		if err != nil {
			r.err = fmt.Errorf("test: %w", err)
			return r
		}
		// Test failures may reference files outside --changed
		// (e.g. the test file itself); keep them.
		r.findings = append(r.findings, tests...)
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

const goimportsMissingNote = "gocheck: goimports not installed; skipping (go install golang.org/x/tools/cmd/goimports@latest)"

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}
