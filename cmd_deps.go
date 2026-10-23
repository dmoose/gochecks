package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// dep is one dependency that has a newer release.
type dep struct {
	Kind    string `json:"kind"` // module or asset
	Name    string `json:"name"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Own     bool   `json:"own,omitempty"` // same host and owner as the main module
}

// runDeps reports direct Go modules, copied assets and workflow version
// pins with a newer release. Modules come from go list -m -u, assets
// from the manifest in .gochecks.json, pins from .github/workflows.
// Exit 0 unless --fail and something is behind.
func runDeps(args []string) int {
	fs := flag.NewFlagSet("deps", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		fail    bool
		textOut bool
	)
	fs.BoolVar(&fail, "fail", false, "exit 1 when anything is behind")
	fs.BoolVar(&textOut, "text", false, "human-readable output (default: JSON lines)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cwd, _ := os.Getwd()
	cfg, err := loadConfig(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks deps: config:", err)
		return 2
	}
	deps, err := behindDeps(cwd, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks deps:", err)
		return 2
	}
	if textOut {
		for _, d := range deps {
			fmt.Println(d.text())
		}
	} else {
		enc := json.NewEncoder(os.Stdout)
		for _, d := range deps {
			_ = enc.Encode(d)
		}
	}
	if fail && len(deps) > 0 {
		return 1
	}
	return 0
}

func (d dep) text() string {
	s := fmt.Sprintf("%s %s %s -> %s", d.Kind, d.Name, d.Current, d.Latest)
	if d.Own {
		s += " (own)"
	}
	return s
}

// behindDeps lists modules, then assets, then workflow pins.
func behindDeps(dir string, cfg Config) ([]dep, error) {
	mods, err := modulesBehind(dir)
	if err != nil {
		return nil, err
	}
	assets, err := assetsBehind(dir, cfg)
	if err != nil {
		return nil, err
	}
	wf, err := workflowsBehind(moduleRoot(dir), githubLatestRelease, latestModuleVersion)
	if err != nil {
		return nil, err
	}
	return append(append(mods, assets...), wf...), nil
}

// moduleRoot is the directory of the nearest go.mod at or above dir, or
// dir itself when there is none.
func moduleRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

// goModule is the part of go list -m -u -json read here.
type goModule struct {
	Path     string
	Version  string
	Main     bool
	Indirect bool
	Update   *struct{ Version string }
}

// modulesBehind returns nothing outside a module; assets and workflow
// pins are still checked there.
func modulesBehind(dir string) ([]dep, error) {
	if modulePath(dir) == "" {
		return nil, nil
	}
	cmd := exec.Command("go", "list", "-m", "-u", "-json", "all")
	cmd.Dir = dir
	// GOFLAGS is cleared so a -mod setting from the environment cannot
	// break the listing. GOWORK=off keeps the listing to this module's
	// own go.mod inside a workspace.
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			return nil, fmt.Errorf("go list -m -u: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return parseModulesBehind(string(out)), nil
}

// parseModulesBehind reads the concatenated JSON objects go list prints
// and keeps direct dependencies with an update, own modules first.
func parseModulesBehind(out string) []dep {
	dec := json.NewDecoder(strings.NewReader(out))
	var mods []goModule
	var main string
	for {
		var m goModule
		if err := dec.Decode(&m); err != nil {
			break
		}
		if m.Main {
			main = m.Path
			continue
		}
		mods = append(mods, m)
	}
	owner := ownerPrefix(main)
	var deps []dep
	for _, m := range mods {
		if m.Indirect || m.Update == nil || m.Update.Version == m.Version {
			continue
		}
		deps = append(deps, dep{
			Kind:    "module",
			Name:    m.Path,
			Current: m.Version,
			Latest:  m.Update.Version,
			Own:     owner != "" && strings.HasPrefix(m.Path, owner),
		})
	}
	slices.SortStableFunc(deps, func(a, b dep) int {
		if a.Own != b.Own {
			if a.Own {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return deps
}

// ownerPrefix returns "host/owner/" for a module path, the part shared
// by a repo's own modules.
func ownerPrefix(module string) string {
	parts := strings.SplitN(module, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1] + "/"
}

// assetsBehind checks each file in the manifest against the npm
// registry. The file's version is read from its header.
func assetsBehind(dir string, cfg Config) ([]dep, error) {
	if len(cfg.Assets) == 0 {
		return nil, nil
	}
	root := dir
	if cfg.Path != "" {
		root = filepath.Dir(cfg.Path)
	}
	names := slices.Sorted(mapKeys(cfg.Assets))
	var deps []dep
	for _, rel := range names {
		pkg := cfg.Assets[rel]
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", rel, err)
		}
		current := sniffVersion(data)
		latest, err := npmLatest(pkg)
		if err != nil {
			return nil, fmt.Errorf("asset %s (%s): %w", rel, pkg, err)
		}
		if current == "" {
			deps = append(deps, dep{Kind: "asset", Name: rel, Current: "unknown", Latest: latest})
			continue
		}
		if current != latest {
			deps = append(deps, dep{Kind: "asset", Name: rel, Current: current, Latest: latest})
		}
	}
	return deps, nil
}

func mapKeys(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

var (
	versionTagRE = regexp.MustCompile(`(?i)(?:version\s*[:=]\s*["']?|\bv)(\d+\.\d+\.\d+)`)
	semverRE     = regexp.MustCompile(`\d+\.\d+\.\d+`)
)

// sniffVersion finds the version a bundled asset announces in its
// header: a "version: x.y.z" or "vx.y.z" form first, else the first
// dotted triple in the first 8 KiB.
func sniffVersion(data []byte) string {
	head := data[:min(len(data), 8<<10)]
	if m := versionTagRE.FindSubmatch(head); m != nil {
		return string(m[1])
	}
	return string(semverRE.Find(head))
}

func npmLatest(pkg string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/"+pkg+"/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry: %s", resp.Status)
	}
	var v struct{ Version string }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// depsSummary is the one-line form used at session start.
func depsSummary(deps []dep) string {
	if len(deps) == 0 {
		return ""
	}
	parts := make([]string, 0, len(deps))
	for _, d := range deps {
		parts = append(parts, fmt.Sprintf("%s %s -> %s", filepath.Base(d.Name), d.Current, d.Latest))
	}
	return fmt.Sprintf("gochecks: %d dependencies behind: %s (gochecks deps --text)", len(deps), strings.Join(parts, ", "))
}
