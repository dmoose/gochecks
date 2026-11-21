package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// stopRepos returns the git repos a Stop run checks: the repo at cwd,
// else, at a go.work root, the repo of each module it uses.
func stopRepos(cwd string) []string {
	if top := gitTop(cwd); top != "" {
		return []string{top}
	}
	if _, err := os.Stat(filepath.Join(cwd, "go.work")); err != nil {
		return nil
	}
	var repos []string
	for _, use := range workUses(cwd) {
		if top := gitTop(use); top != "" && !slices.Contains(repos, top) {
			repos = append(repos, top)
		}
	}
	slices.Sort(repos)
	return repos
}

// gitTop is the top level of the repo holding dir, or "".
func gitTop(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	top := strings.TrimSpace(string(out))
	// git resolves symlinks; keep dir's spelling when it is the top.
	if samePath(top, dir) {
		return dir
	}
	return top
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// workUses lists the absolute directories the go.work in dir uses.
func workUses(dir string) []string {
	cmd := exec.Command("go", "work", "edit", "-json")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var w struct{ Use []struct{ DiskPath string } }
	if json.Unmarshal(out, &w) != nil {
		return nil
	}
	var dirs []string
	for _, u := range w.Use {
		p := u.DiskPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		dirs = append(dirs, filepath.Clean(p))
	}
	return dirs
}

// changedGoFiles lists the .go files in repo that differ from HEAD or
// are untracked and not ignored, as absolute paths. These include
// edits made through Bash, which PostToolUse never sees. A repo with
// no commit yet counts every file as changed.
func changedGoFiles(repo string) []string {
	var names []string
	diff := []string{"diff", "--name-only", "-z", "HEAD"}
	if exec.Command("git", "-C", repo, "rev-parse", "--verify", "-q", "HEAD").Run() != nil {
		diff = []string{"ls-files", "-z"}
	}
	for _, args := range [][]string{diff, {"ls-files", "-o", "--exclude-standard", "-z"}} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output() //nolint:gosec // fixed git subcommands; repo is a git top level
		if err != nil {
			continue
		}
		for name := range strings.SplitSeq(string(out), "\x00") {
			if strings.HasSuffix(name, ".go") {
				names = append(names, filepath.Join(repo, name))
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
