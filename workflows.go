package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// pin is a version pinned in a GitHub Actions workflow file.
type pin struct {
	file    string // workflow file, relative to the repo root
	kind    string // action, lint, or tool
	name    string // owner/repo, or the Go package
	version string // as written: v6, v6.1.0, 2.15.0
}

var (
	usesRE      = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*["']?([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)(?:/[^@\s"']+)?@([^\s"'#]+)`)
	lintVerRE   = regexp.MustCompile(`(?m)^\s*version:\s*["']?v?(\d+\.\d+\.\d+)`)
	goInstallRE = regexp.MustCompile(`go install\s+([^\s@]+)@v?(\d+\.\d+\.\d+)`)
	shaRE       = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// parseWorkflowPins reads the version pins out of one workflow file:
// `uses: owner/repo@vN`, golangci-lint-action's `version:` setting, and
// `go install pkg@vX.Y.Z` in run steps. Pins by commit hash are skipped;
// they are a deliberate choice.
func parseWorkflowPins(file, content string) []pin {
	var pins []pin
	seen := map[string]bool{}
	add := func(p pin) {
		key := p.kind + p.name + p.version
		if !seen[key] {
			seen[key] = true
			pins = append(pins, p)
		}
	}
	for _, m := range usesRE.FindAllStringSubmatch(content, -1) {
		if shaRE.MatchString(m[2]) || strings.HasPrefix(m[1], ".") {
			continue
		}
		add(pin{file: file, kind: "action", name: m[1], version: m[2]})
	}
	// golangci-lint-action's version setting belongs to the step that
	// uses the action; take the first version: after each such uses:.
	for _, loc := range usesRE.FindAllStringSubmatchIndex(content, -1) {
		if content[loc[2]:loc[3]] != "golangci/golangci-lint-action" {
			continue
		}
		rest := content[loc[1]:]
		if next := usesRE.FindStringIndex(rest); next != nil {
			rest = rest[:next[0]]
		}
		if m := lintVerRE.FindStringSubmatch(rest); m != nil {
			add(pin{file: file, kind: "lint", name: "golangci-lint", version: "v" + m[1]})
		}
	}
	for _, m := range goInstallRE.FindAllStringSubmatch(content, -1) {
		add(pin{file: file, kind: "tool", name: m[1], version: "v" + m[2]})
	}
	return pins
}

// workflowsBehind checks every pin under .github/workflows against the
// latest release. Actions pinned to a major (v6) are behind only when a
// newer major exists; full versions are compared exactly.
func workflowsBehind(root string, latestAction func(ownerRepo string) (string, error), latestModule func(module string) string) ([]dep, error) {
	files, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	var deps []dep
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(root, f)
		for _, p := range parseWorkflowPins(rel, string(data)) {
			var latest string
			switch p.kind {
			case "action":
				latest, err = latestAction(p.name)
				if err != nil {
					return nil, fmt.Errorf("%s: %s: %w", rel, p.name, err)
				}
			case "lint":
				latest = latestModule("github.com/golangci/golangci-lint/v2")
			case "tool":
				latest = latestModule(moduleForPackage(p.name))
			}
			if latest == "" || !versionBehind(p.version, latest) {
				continue
			}
			deps = append(deps, dep{Kind: "workflow", Name: p.name + " (" + filepath.Base(rel) + ")", Current: p.version, Latest: latest})
		}
	}
	return deps, nil
}

// moduleForPackage maps a go install path to its module when the tool
// is one gochecks knows; "" otherwise.
func moduleForPackage(pkg string) string {
	for _, t := range toolSpecs {
		if t.pkg == pkg {
			return t.module
		}
	}
	return ""
}

// versionBehind compares a pinned version with the latest. A major-only
// pin (v6) is behind when latest has a higher major.
func versionBehind(pinned, latest string) bool {
	p, l := versionParts(pinned), versionParts(latest)
	if len(p) == 0 || len(l) == 0 {
		return false
	}
	for i := range min(len(p), len(l)) {
		if p[i] != l[i] {
			return p[i] < l[i]
		}
	}
	return false
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for part := range strings.SplitSeq(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// githubLatestRelease returns the tag of owner/repo's latest release.
// GITHUB_TOKEN, when set, lifts the unauthenticated rate limit.
func githubLatestRelease(ownerRepo string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+ownerRepo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil // no releases; nothing to compare
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api: %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", err
	}
	return rel.TagName, nil
}
