package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	fs.BoolVar(&noApply, "no-apply", false, "write nothing; report what would be applied")
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
	code := 0
	if len(files) > 0 {
		cfg, err := loadConfig(filepath.Dir(files[0]))
		if err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: config:", err)
			return 2
		}
		if files = cfg.applyExclude(files); len(files) > 0 {
			code = runCheckWith(files, noApply, textOut, cfg)
		}
	}
	return missingExit("check", raw, code)
}

// missingExit names on stderr each path in raw that does not exist
// and returns 2 if any, else code. It runs after the files that exist
// were checked, so a mistyped path neither reads as a clean run nor
// hides their findings.
func missingExit(cmd string, raw []string, code int) int {
	missing := missingFiles(raw)
	for _, p := range missing {
		fmt.Fprintf(os.Stderr, "gochecks %s: no such file: %s\n", cmd, p)
	}
	if len(missing) > 0 {
		return 2
	}
	return code
}

// missingFiles returns the paths in raw that do not exist, as given.
func missingFiles(raw []string) []string {
	var out []string
	for _, p := range raw {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			out = append(out, p)
		}
	}
	return out
}

func runCheckWith(files []string, noApply, textOut bool, cfg Config) int {
	findings, skipped, err := check(files, noApply, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks:", err)
		return 2
	}
	invocationFindings = len(findings)
	emit(findings, textOut)
	if textOut {
		cwd, _ := os.Getwd()
		fmt.Print(skippedNote(skipped, cwd, cfg))
	}
	return exitFor(findings)
}

// check formats (or reports formatting for) files, then runs the
// per-package analyzers and returns the scoped findings, and the
// packages whose analyzers did not run because they do not compile.
//
// noApply writes nothing: formatting is reported, and auto_fix is off,
// so golangci-lint runs without --fix and modernize rewrites are listed
// instead of applied.
func check(files []string, noApply bool, cfg Config) (findings []Finding, skipped []string, err error) {
	if noApply {
		cfg.AutoFix = false
	}

	if noApply {
		fmtFindings, err := runGofmt(files, false)
		if err != nil {
			return nil, nil, fmt.Errorf("gofmt: %w", err)
		}
		findings = append(findings, fmtFindings...)

		impFindings, err := runGoimports(files, false)
		switch {
		case errors.Is(err, errGoimportsMissing):
			fmt.Fprintln(os.Stderr, goimportsMissingNote)
		case err != nil:
			return nil, nil, fmt.Errorf("goimports: %w", err)
		default:
			findings = append(findings, impFindings...)
		}
	} else {
		fs, err := applyFormat(files)
		if err != nil {
			return nil, nil, err
		}
		findings = append(findings, fs...)
	}

	scope := make(map[string]bool, len(files))
	for _, f := range files {
		scope[f] = true
	}

	pkgs := groupByPackage(files)
	pkgFindings, skipped, err := analyzePackages(pkgs, scope, cfg)
	if err != nil {
		return nil, nil, err
	}
	findings = append(findings, pkgFindings...)

	// Modernize only a tree with nothing blocking in it.
	findings = dropParseDupes(findings)
	if cfg.AutoFix && !hasBlockingFinding(findings) {
		for pkgDir := range pkgs {
			if err := modernizeAndFormat(pkgDir, scope); err != nil {
				return nil, nil, fmt.Errorf("auto_fix modernize: %w", err)
			}
			findings = append(findings, filterScope(modernizeConflicts(pkgDir), scope)...)
		}
	}
	return findings, skipped, nil
}

// modernizeAndFormat applies go fix to the files in scope, then formats
// the ones it changed: a rewrite can leave what the formatter removes,
// such as a blank first line in a loop body.
func modernizeAndFormat(pkgDir string, scope map[string]bool) error {
	changed, err := runModernize(pkgDir, scope)
	if err != nil || len(changed) == 0 {
		return err
	}
	_, err = applyFormat(changed)
	return err
}

type pkgResult struct {
	findings []Finding
	pkgDir   string
	// skipped: the package does not compile, so its analyzers did not run.
	skipped bool
	err     error
}

// analyzePackages runs the analyzers per package, NumCPU at a time.
func analyzePackages(pkgs map[string][]string, scope map[string]bool, cfg Config) ([]Finding, []string, error) {
	probes := toolProbes{
		staticcheck: staticcheckAvailable(),
		errcheck:    errcheckAvailable(),
		ineffassign: ineffassignAvailable(),
		golangci:    cfg.lintersOn() && golangciLintAvailable(),
	}
	// golangci-lint runs staticcheck, errcheck and ineffassign itself.
	if probes.golangci {
		probes.staticcheck = false
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
	var skipped []string
	for r := range results {
		if r.err != nil {
			return nil, nil, r.err
		}
		findings = append(findings, r.findings...)
		if r.skipped {
			skipped = append(skipped, r.pkgDir)
		}
	}
	slices.Sort(skipped)
	findings = append(findings, lintersMissing(cfg)...)
	return dedupeFailed(collapseBroken(dropGovetDupes(findings))), skipped, nil
}

// dropGovetDupes drops golangci-lint's govet finding where go vet, which
// always runs, reported the same position and message. vet's is kept.
func dropGovetDupes(findings []Finding) []Finding {
	type key struct {
		file      string
		line, col int
		msg       string
	}
	vet := map[key]bool{}
	for _, f := range findings {
		if f.Tool == "vet" {
			vet[key{f.File, f.Line, f.Col, f.Msg}] = true
		}
	}
	out := findings[:0]
	for _, f := range findings {
		if f.Tool == "golangci-lint/govet" {
			// govet prefixes the analyzer: "printf: fmt.Printf format ..."
			_, msg, _ := strings.Cut(f.Msg, ": ")
			if vet[key{f.File, f.Line, f.Col, msg}] || vet[key{f.File, f.Line, f.Col, f.Msg}] {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// skippedNote says which packages vet and the linters did not run on
// because they do not compile, so a report with build errors is not
// read as the whole story. Paths are relative to cwd.
func skippedNote(skipped []string, cwd string, cfg Config) string {
	if len(skipped) == 0 {
		return ""
	}
	dirs := make([]string, len(skipped))
	for i, d := range skipped {
		dirs[i] = relPath(d, cwd) + "/"
	}
	what := "vet and the linters"
	if cfg.Test {
		what = "vet, the linters and go test"
	}
	return fmt.Sprintf("%s did not run on %s: it does not compile. They run once it does.\n", what, strings.Join(dirs, ", "))
}

// lintersMissing is a warning when the config asks for golangci-lint
// and it is not on PATH. Absent other analyzers are skipped: doctor
// reports them, and nothing asked for them.
func lintersMissing(cfg Config) []Finding {
	if cfg.Linters == nil || !*cfg.Linters || golangciLintAvailable() {
		return nil
	}
	return []Finding{{
		File:   cfg.Path,
		Class:  "warning",
		Tool:   "golangci-lint",
		Msg:    "\"linters\": true, but golangci-lint is not on PATH; run `gochecks doctor --fix`",
		failed: true,
	}}
}

type toolProbes struct {
	staticcheck, errcheck, ineffassign, golangci bool
}

func analyzeOne(pkgDir string, scope map[string]bool, cfg Config, p toolProbes) pkgResult {
	r := pkgResult{pkgDir: pkgDir}
	build := runBuild(pkgDir)
	r.findings = buildInScope(build, pkgDir, scope)
	// Analyzers on a package that does not compile only repeat the
	// type errors. A module go refuses to load has its own finding.
	if len(build) > 0 {
		r.skipped = !build[0].failed
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

// buildInScope keeps the build errors in any file of pkgDir, changed
// or not: a change that breaks a caller elsewhere in its own package
// leaves nothing checked, and is the changer's to fix. Errors in other
// packages are scoped as usual.
func buildInScope(build []Finding, pkgDir string, scope map[string]bool) []Finding {
	var out []Finding
	for _, f := range build {
		if f.failed || scope[f.File] || filepath.Dir(f.File) == pkgDir {
			out = append(out, f)
		}
	}
	return out
}

func hasBlockingFinding(findings []Finding) bool {
	for _, f := range findings {
		if f.Class == "error" || f.Class == "warning" {
			return true
		}
	}
	return false
}

// applyFormat formats files in place and returns the parse errors of
// those that could not be.
func applyFormat(files []string) ([]Finding, error) {
	fs, err := runGofmt(files, true)
	if err != nil {
		return nil, fmt.Errorf("gofmt: %w", err)
	}
	imp, err := runGoimports(files, true)
	if errors.Is(err, errGoimportsMissing) {
		fmt.Fprintln(os.Stderr, goimportsMissingNote)
		return fs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("goimports: %w", err)
	}
	return append(fs, imp...), nil
}

var formatters = map[string]bool{"gofmt": true, "gofumpt": true, "goimports": true}

// dropParseDupes keeps one report of a syntax error: the formatters and
// the build all see it at the same position. The build's is kept.
func dropParseDupes(findings []Finding) []Finding {
	type pos struct {
		file      string
		line, col int
	}
	other := map[pos]bool{}
	for _, f := range findings {
		if f.Line > 0 && !formatters[f.Tool] {
			other[pos{f.File, f.Line, f.Col}] = true
		}
	}
	seen := map[pos]bool{}
	out := findings[:0]
	for _, f := range findings {
		p := pos{f.File, f.Line, f.Col}
		if formatters[f.Tool] && f.Line > 0 {
			if other[p] || seen[p] {
				continue
			}
			seen[p] = true
		}
		out = append(out, f)
	}
	return out
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
