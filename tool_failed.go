package main

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// failedRun turns a tool run that produced no findings into one, when
// the tool could not start or exited non-zero. A tool that loads
// nothing must never read as a clean pass. detail is the output that
// says why; class is error for the build, warning for an analyzer.
func failedRun(r toolRun, pkgDir, tool, class, detail string) []Finding {
	f := Finding{File: pkgDir, Class: class, Tool: tool, failed: true}
	switch {
	case r.startErr != nil:
		f.Msg = "did not run: " + r.startErr.Error()
	case r.code == 0:
		return nil
	case strings.Contains(detail, "not one of the workspace modules listed in go.work"):
		f.File, f.Msg = goWorkMsg(pkgDir)
	default:
		f.Msg = "exited " + strconv.Itoa(r.code) + " without findings: " + summarize(detail)
	}
	return []Finding{f}
}

// goWorkMsg names the module go refused, from its root, and the go.work
// line that would admit it.
func goWorkMsg(pkgDir string) (dir, msg string) {
	dir = moduleRoot(pkgDir)
	work := goEnv(pkgDir, "GOWORK")
	if work == "" || work == "off" {
		return dir, "module is not listed in the enclosing go.work, so go will not load it"
	}
	use := dir
	if rel, err := filepath.Rel(filepath.Dir(work), dir); err == nil {
		use = "./" + filepath.ToSlash(rel)
	}
	return dir, "module is not listed in " + work + ", so go will not load it: add `use " + use + "` there"
}

func goEnv(dir, key string) string {
	cmd := exec.Command("go", "env", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// summarize keeps the first lines of a tool's error output.
func summarize(out string) string {
	var lines []string
	for line := range strings.SplitSeq(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
		if len(lines) == 3 {
			break
		}
	}
	if len(lines) == 0 {
		return "no output"
	}
	s := strings.Join(lines, "; ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// dedupeFailed keeps one failed-run finding per directory, tool and
// message: every package of a refused module reports the same thing.
func dedupeFailed(findings []Finding) []Finding {
	type key struct{ file, tool, msg string }
	seen := map[key]bool{}
	out := findings[:0]
	for _, f := range findings {
		if f.failed {
			k := key{f.File, f.Tool, f.Msg}
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		out = append(out, f)
	}
	return out
}
