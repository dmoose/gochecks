package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// auditItem is one thing that turns a check off or freezes something.
type auditItem struct {
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Msg  string `json:"msg"`
}

// Kinds, in report order.
const (
	kindNolint      = "nolint"
	kindLintIgnore  = "lint-ignore"
	kindGolangci    = "golangci-config"
	kindConfig      = "gochecks-config"
	kindGoworkOff   = "gowork-off"
	kindReplace     = "replace"
	kindDepBehind   = "dep-behind"
	kindMakeCheck   = "make-check"
	kindAllow       = "allow"
	kindSuppression = "suppression" // summary only: nolint + lint-ignore
)

var kindOrder = []string{kindNolint, kindLintIgnore, kindGolangci, kindConfig, kindGoworkOff, kindReplace, kindDepBehind, kindMakeCheck, kindAllow}

func runAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		textOut bool
		offline bool
	)
	fs.BoolVar(&textOut, "text", false, "human-readable output (default: JSON lines)")
	fs.BoolVar(&offline, "offline", false, "skip dependencies behind (needs the network)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks audit:", err)
		return 2
	}
	items, err := audit(abs, !offline)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks audit:", err)
		return 2
	}
	invocationFindings = len(items)
	writeAudit(os.Stdout, items, abs, textOut)
	return 0
}

func writeAudit(out io.Writer, items []auditItem, root string, textOut bool) {
	if !textOut {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		for _, it := range items {
			_ = enc.Encode(it)
		}
		return
	}
	for _, it := range items {
		path := it.File
		if rel, err := filepath.Rel(root, it.File); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
		if it.Line > 0 {
			path = fmt.Sprintf("%s:%d", path, it.Line)
		}
		_, _ = fmt.Fprintf(out, "%s %s: %s\n", path, it.Kind, it.Msg)
	}
}

// audit walks root and lists everything that silences or pins. deps
// adds dependencies behind, which needs the network.
func audit(root string, deps bool) ([]auditItem, error) {
	var items []auditItem
	var modules []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != root {
				return filepath.SkipDir
			}
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (skipScanDir(name) || gitIgnored(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(name, ".go"):
			items = append(items, auditGoFile(path)...)
		case name == "go.mod":
			modules = append(modules, filepath.Dir(path))
			items = append(items, auditReplace(path)...)
		case name == configFilename:
			items = append(items, auditConfig(path)...)
		case slices.Contains(lintConfigNames, name):
			items = append(items, auditGolangci(path)...)
		case name == "Makefile" || name == "GNUmakefile" || strings.HasSuffix(name, ".mk"):
			items = append(items, auditGoworkOff(path)...)
			if name != "Makefile" && name != "GNUmakefile" {
				break
			}
			items = append(items, auditMakeCheck(path)...)
		case name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.") || strings.HasSuffix(name, ".Dockerfile"):
			items = append(items, auditGoworkOff(path)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if deps {
		for _, m := range modules {
			items = append(items, auditDeps(m)...)
		}
	}
	items = append(items, auditAllows(root)...)
	slices.SortStableFunc(items, func(a, b auditItem) int {
		if c := slices.Index(kindOrder, a.Kind) - slices.Index(kindOrder, b.Kind); c != 0 {
			return c
		}
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
	return items, nil
}

var (
	nolintRE     = regexp.MustCompile(`^//\s*nolint(?::([\w,\- ]+?))?\s*(?://\s*(.*))?$`)
	lintIgnoreRE = regexp.MustCompile(`^//lint:(ignore|file-ignore)\s+(\S+)\s*(.*)$`)
)

// auditGoFile lists the suppression comments in one Go file. Generated
// files are skipped: nobody wrote their comments.
func auditGoFile(path string) []auditItem {
	if gen, err := isGenerated(path); err != nil || gen {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var items []auditItem
	for _, group := range f.Comments {
		for _, c := range group.List {
			line := fset.Position(c.Pos()).Line
			text := strings.TrimSpace(c.Text)
			// A trailing //nolint shares its comment with nothing, but a
			// reason after it is part of the same // comment.
			if m := nolintRE.FindStringSubmatch(text); m != nil {
				items = append(items, auditItem{Kind: kindNolint, File: path, Line: line, Msg: describeSuppression(strings.TrimSpace(m[1]), strings.TrimSpace(m[2]))})
				continue
			}
			if m := lintIgnoreRE.FindStringSubmatch(text); m != nil {
				what := m[2]
				if m[1] == "file-ignore" {
					what += " (whole file)"
				}
				items = append(items, auditItem{Kind: kindLintIgnore, File: path, Line: line, Msg: describeSuppression(what, strings.TrimSpace(m[3]))})
			}
		}
	}
	return items
}

func describeSuppression(linters, reason string) string {
	if linters == "" {
		linters = "all linters"
	}
	if reason == "" {
		reason = "no reason given"
	}
	return linters + ": " + reason
}

// auditReplace lists replace directives, which pin a module to a fork
// or a local copy.
func auditReplace(path string) []auditItem {
	var items []auditItem
	inBlock := false
	eachLine(path, func(n int, line string) {
		code, _, _ := strings.Cut(line, "//")
		code = strings.TrimSpace(code)
		switch {
		case inBlock && code == ")":
			inBlock = false
		case inBlock && code != "":
			items = append(items, auditItem{Kind: kindReplace, File: path, Line: n, Msg: code})
		case code == "replace (":
			inBlock = true
		case strings.HasPrefix(code, "replace "):
			items = append(items, auditItem{Kind: kindReplace, File: path, Line: n, Msg: strings.TrimSpace(strings.TrimPrefix(code, "replace "))})
		}
	})
	return items
}

// auditConfig lists the .gochecks.json settings that turn a check off.
func auditConfig(path string) []auditItem {
	var raw map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &raw) != nil {
		return nil
	}
	var cfg Config
	_ = json.Unmarshal(data, &cfg)
	var items []auditItem
	add := func(msg string) { items = append(items, auditItem{Kind: kindConfig, File: path, Msg: msg}) }
	if cfg.Linters != nil && !*cfg.Linters {
		add(`"linters": false: golangci-lint does not run`)
	}
	for _, pat := range cfg.Exclude {
		add(fmt.Sprintf("exclude %q: matching files are never checked", pat))
	}
	if !cfg.Test && hasTests(filepath.Dir(path)) {
		add(`"test" is off: go test does not run on changed packages, and this tree has tests`)
	}
	return items
}

// hasTests reports whether dir holds a _test.go file outside the
// directories the go tool skips.
func hasTests(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if d.IsDir() && path != dir && skipScanDir(d.Name()) {
			return filepath.SkipDir
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

var goworkOffRE = regexp.MustCompile(`\bGOWORK\s*[:?]?=\s*off\b`)

func auditGoworkOff(path string) []auditItem {
	var items []auditItem
	eachLine(path, func(n int, line string) {
		if code, _, _ := strings.Cut(line, "#"); goworkOffRE.MatchString(code) {
			items = append(items, auditItem{Kind: kindGoworkOff, File: path, Line: n, Msg: "GOWORK=off: builds outside the workspace, against other dependency versions"})
		}
	})
	return items
}

// makeTarget is one rule of a Makefile: its prerequisites and recipe.
type makeTarget struct {
	line    int
	prereqs []string
	recipe  []string
}

var (
	makeRuleRE   = regexp.MustCompile(`^([A-Za-z0-9_.\-/ ]+?)\s*::?\s*([^=].*)?$`)
	makeAssignRE = regexp.MustCompile(`^\s*(?:export\s+|override\s+)?([A-Za-z0-9_]+)\s*(?::{1,3}|\?|\+|!)?=\s*(.*)$`)
	makeVarRE    = regexp.MustCompile(`\$[({]([A-Za-z0-9_]+)[)}]`)
	gochecksRE   = regexp.MustCompile(`\bgochecks\b`)
)

// parseMakefile reads rules and simple variable assignments; recipes
// come back with $(VAR) expanded, so `./$(BINARY) check` is seen.
func parseMakefile(path string) map[string]*makeTarget {
	targets := map[string]*makeTarget{}
	vars := map[string]string{}
	var cur []*makeTarget
	eachLine(path, func(n int, line string) {
		if !strings.HasPrefix(line, "\t") {
			if m := makeAssignRE.FindStringSubmatch(line); m != nil {
				val, _, _ := strings.Cut(m[2], "#")
				vars[m[1]] = strings.TrimSpace(val)
				cur = nil
				return
			}
		}
		if strings.HasPrefix(line, "\t") {
			for _, t := range cur {
				t.recipe = append(t.recipe, line)
			}
			return
		}
		code, _, _ := strings.Cut(line, "#")
		m := makeRuleRE.FindStringSubmatch(strings.TrimRight(code, " "))
		if m == nil {
			if strings.TrimSpace(code) != "" {
				cur = nil
			}
			return
		}
		cur = nil
		prereqs, _, _ := strings.Cut(m[2], ";")
		for name := range strings.FieldsSeq(m[1]) {
			t := targets[name]
			if t == nil {
				t = &makeTarget{line: n}
				targets[name] = t
			}
			t.prereqs = append(t.prereqs, strings.Fields(prereqs)...)
			cur = append(cur, t)
		}
	})
	expand := func(s string) string {
		for range 3 {
			s = makeVarRE.ReplaceAllStringFunc(s, func(v string) string {
				if val, ok := vars[makeVarRE.FindStringSubmatch(v)[1]]; ok {
					return val
				}
				return v
			})
		}
		return s
	}
	for _, t := range targets {
		for i, line := range t.recipe {
			t.recipe[i] = expand(line)
		}
	}
	return targets
}

// auditMakeCheck lists a check target that does not run gochecks, or
// runs it under the old name, following prerequisites in the same
// Makefile.
func auditMakeCheck(path string) []auditItem {
	targets := parseMakefile(path)
	check := targets["check"]
	if check == nil {
		return nil
	}
	seen := map[string]bool{}
	var runs, old bool
	var walk func(name string)
	walk = func(name string) {
		t := targets[name]
		if t == nil || seen[name] {
			return
		}
		seen[name] = true
		for _, line := range t.recipe {
			code, _, _ := strings.Cut(line, "#")
			runs = runs || gochecksRE.MatchString(code)
			old = old || oldNameRE.MatchString(code)
		}
		for _, p := range t.prereqs {
			walk(p)
		}
	}
	walk("check")
	switch {
	case old:
		return []auditItem{{Kind: kindMakeCheck, File: path, Line: check.line, Msg: "check runs gocheck, the old name; use gochecks"}}
	case !runs:
		return []auditItem{{Kind: kindMakeCheck, File: path, Line: check.line, Msg: "check does not run gochecks"}}
	}
	return nil
}

func auditDeps(dir string) []auditItem {
	cfg, err := loadConfig(dir)
	if err != nil {
		return nil
	}
	deps, err := behindDeps(dir, cfg)
	if err != nil {
		return []auditItem{{Kind: kindDepBehind, File: filepath.Join(dir, "go.mod"), Msg: "could not check dependencies: " + err.Error()}}
	}
	items := make([]auditItem, 0, len(deps))
	for _, d := range deps {
		items = append(items, auditItem{Kind: kindDepBehind, File: filepath.Join(dir, "go.mod"), Msg: d.text()})
	}
	return items
}

var allowLineRE = regexp.MustCompile(`^(\S+) granted session=(\S+) hash=\S+ cwd=(.*) findings=(\d+) reason=(".*")$`)

// auditAllows lists Stop-hook releases granted for sessions in root or
// below it.
func auditAllows(root string) []auditItem {
	dir, err := stopDir("")
	if err != nil {
		return nil
	}
	path := filepath.Join(dir, "allow.log")
	var items []auditItem
	eachLine(path, func(_ int, line string) {
		m := allowLineRE.FindStringSubmatch(line)
		if m == nil {
			return
		}
		if rel, err := filepath.Rel(root, m[3]); err != nil || strings.HasPrefix(rel, "..") {
			return
		}
		items = append(items, auditItem{Kind: kindAllow, File: m[3], Msg: fmt.Sprintf("%s: stop allowed past %s finding(s), session %s, reason %s", m[1], m[4], m[2], m[5])})
	})
	return items
}

func eachLine(path string, fn func(n int, line string)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		fn(n, sc.Text())
	}
}

// auditSummary is the one SessionStart line, or "" when there is
// nothing. Dependencies are left out: the deps line covers them.
func auditSummary(dir string) string {
	items, err := audit(dir, false)
	if err != nil || len(items) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, it := range items {
		k := it.Kind
		if k == kindNolint || k == kindLintIgnore {
			k = kindSuppression
		}
		counts[k]++
	}
	labels := []struct{ kind, label string }{
		{kindSuppression, "suppression(s)"},
		{kindGolangci, "lint config change(s)"},
		{kindConfig, "gochecks setting(s) off"},
		{kindGoworkOff, "GOWORK=off"},
		{kindReplace, "replace(s)"},
		{kindMakeCheck, "make check gap(s)"},
		{kindAllow, "stop(s) allowed"},
	}
	var parts []string
	for _, l := range labels {
		if n := counts[l.kind]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, l.label))
		}
	}
	return "gochecks: " + strings.Join(parts, ", ") + ": gochecks audit --text\n"
}
