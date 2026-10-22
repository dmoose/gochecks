package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	goversion "go/version"
)

// toolReport is the doctor's findings for one tool.
type toolReport struct {
	spec      toolSpec
	path      string // "" when not on PATH
	installed buildInfo
	latest    string // "" when unknown (offline or lookup failed)
	broken    bool   // typechecking fails against the current toolchain
}

func (r toolReport) outdated() bool {
	return r.latest != "" && r.installed.version != "" && r.installed.version != r.latest
}

// needsInstall reports whether --fix should install this tool.
func (r toolReport) needsInstall(cfg Config) bool {
	if r.path == "" {
		return r.spec.optIn == "" || (r.spec.optIn == "linters" && cfg.Linters)
	}
	return r.broken || r.outdated()
}

// runDoctor checks the Go toolchain and each tool: present, built
// against the current toolchain, and at its module's latest release.
// --fix runs the installs. Exit 1 when anything needs attention.
func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		fix     bool
		offline bool
	)
	fs.BoolVar(&fix, "fix", false, "install missing, broken, or outdated tools with go install @latest")
	fs.BoolVar(&offline, "offline", false, "skip latest-version lookups")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cwd, _ := os.Getwd()
	cfg, _ := loadConfig(cwd)

	goVer, goPath := goToolchain()
	fmt.Printf("gochecks %s (%s)\n\n", versionString(), selfPath())

	var latestGo string
	if !offline {
		latestGo = latestGoRelease()
	}
	fmt.Printf("%-14s %-32s %s\n", "go", goVer+"  "+goPath, verdictGo(goVer, latestGo))

	reports := diagnoseTools(goVer, offline)
	attention := latestGo != "" && goversion.Compare(goVer, latestGo) < 0
	var installs []string
	for _, r := range reports {
		fmt.Printf("%-14s %-32s %s\n", r.spec.name, describeInstalled(r), verdictTool(r, cfg, goVer))
		if r.needsInstall(cfg) {
			installs = append(installs, r.spec.pkg+"@latest")
			attention = true
		}
	}

	fmt.Println()
	fmt.Println(repoWiring(cwd, cfg))

	if len(installs) == 0 {
		return exitAttention(attention)
	}
	fmt.Println()
	if !fix {
		// go install pkg@version takes one module per invocation.
		fmt.Println("to update, run gochecks doctor --fix, or:")
		for _, pkg := range installs {
			fmt.Println("  go install " + pkg)
		}
		return 1
	}
	for _, pkg := range installs {
		fmt.Printf("go install %s\n", pkg)
		cmd := exec.Command("go", "install", pkg)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "gochecks doctor: %s: %v\n", pkg, err)
			return 1
		}
	}
	fmt.Println()
	fmt.Println("re-checking:")
	for _, r := range diagnoseTools(goVer, offline) {
		fmt.Printf("%-14s %-32s %s\n", r.spec.name, describeInstalled(r), verdictTool(r, cfg, goVer))
		if r.needsInstall(cfg) {
			attention = true
		}
	}
	return exitAttention(attention)
}

func exitAttention(attention bool) int {
	if attention {
		return 1
	}
	return 0
}

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return "?"
	}
	return p
}

// goToolchain reports the version and path of the go on PATH.
func goToolchain() (ver, path string) {
	path, err := exec.LookPath("go")
	if err != nil {
		return "", "not found"
	}
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return runtime.Version(), path
	}
	return strings.TrimSpace(string(out)), path
}

// latestGoRelease returns the newest stable toolchain per go.dev, or
// "" on any failure.
func latestGoRelease() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://go.dev/dl/?mode=json", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	var releases []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&releases); err != nil {
		return ""
	}
	for _, r := range releases {
		if r.Stable {
			return r.Version
		}
	}
	return ""
}

func verdictGo(have, latest string) string {
	switch {
	case have == "":
		return "MISSING"
	case latest == "":
		return "ok"
	case goversion.Compare(have, latest) < 0:
		return "outdated, latest " + latest + " (https://go.dev/dl)"
	default:
		return "ok, latest"
	}
}

// diagnoseTools probes every tool in parallel.
func diagnoseTools(goVer string, offline bool) []toolReport {
	reports := make([]toolReport, len(toolSpecs))
	var wg sync.WaitGroup
	for i, spec := range toolSpecs {
		wg.Go(func() {
			reports[i] = diagnoseTool(spec, goVer, offline)
		})
	}
	wg.Wait()
	return reports
}

func diagnoseTool(spec toolSpec, goVer string, offline bool) toolReport {
	r := toolReport{spec: spec}
	if !offline {
		r.latest = latestModuleVersion(spec.module)
	}
	path, err := exec.LookPath(spec.name)
	if err != nil {
		return r
	}
	r.path = path
	r.installed, _ = readBuildInfo(path)
	if spec.typechecks {
		r.broken = probeTypecheck(spec.name, goVer)
	}
	return r
}

// latestModuleVersion resolves module@latest through GOPROXY, outside
// any module so go.work and replace directives do not interfere.
func latestModuleVersion(module string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", module+"@latest")
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var m struct{ Version string }
	if json.Unmarshal(out, &m) != nil {
		return ""
	}
	return m.Version
}

// probeTypecheck runs the tool on a trivial package for the current
// toolchain and reports whether it failed on export data.
func probeTypecheck(tool, goVer string) bool {
	dir, err := os.MkdirTemp("", "gochecks-doctor-")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(dir) }()
	lang := strings.TrimPrefix(goversion.Lang(goVer), "go")
	if lang == "" {
		lang = "1.26"
	}
	files := map[string]string{
		"go.mod": "module probe\n\ngo " + lang + "\n",
		"p.go":   "package p\n\nimport \"fmt\"\n\nfunc F() string { return fmt.Sprint(1) }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return false
		}
	}
	args := []string{"."}
	if tool == "golangci-lint" {
		args = []string{"run", "--no-config", "./..."}
	}
	cmd := exec.Command(tool, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	return toolBroken(out.String())
}

func describeInstalled(r toolReport) string {
	if r.path == "" {
		return "not installed"
	}
	v := r.installed.version
	if v == "" {
		v = "unknown version"
	}
	if r.installed.goVersion != "" {
		v += " (built " + r.installed.goVersion + ")"
	}
	return v
}

func verdictTool(r toolReport, cfg Config, goVer string) string {
	var parts []string
	switch {
	case r.path == "" && r.spec.optIn == "linters" && !cfg.Linters:
		return "not installed; opt-in via linters=true"
	case r.path == "":
		parts = append(parts, "MISSING")
	case r.broken:
		parts = append(parts, "BROKEN: built with "+r.installed.goVersion+", cannot read "+goVer+" export data")
	}
	if r.outdated() {
		parts = append(parts, "outdated, latest "+r.latest)
	}
	if len(parts) == 0 {
		if r.latest == "" {
			return "ok"
		}
		return "ok, latest"
	}
	return strings.Join(parts, "; ")
}

// repoWiring describes the hook and config found under cwd.
func repoWiring(cwd string, cfg Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "repo %s\n", cwd)
	settings := filepath.Join(cwd, ".claude", "settings.json")
	data, err := os.ReadFile(settings)
	switch {
	case err != nil:
		b.WriteString("  hook:   not wired (run gochecks install)\n")
	case strings.Contains(string(data), hookCommand):
		b.WriteString("  hook:   wired\n")
	case containsGocheck(string(data)):
		b.WriteString("  hook:   older command in .claude/settings.json (run gochecks install --upgrade)\n")
	default:
		b.WriteString("  hook:   not wired (run gochecks install)\n")
	}
	if cfg.Path == "" {
		b.WriteString("  config: defaults (no .gochecks.json)")
	} else {
		fmt.Fprintf(&b, "  config: %s (test=%t auto_fix=%t linters=%t)", cfg.Path, cfg.Test, cfg.AutoFix, cfg.Linters)
	}
	return b.String()
}
