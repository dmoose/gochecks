package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// hookInput is the part of the hook events read from stdin.
type hookInput struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	StopHookActive bool   `json:"stop_hook_active"`
	ToolName       string `json:"tool_name"`
	CWD            string `json:"cwd"`
	ToolInput      struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// hookOutput is the PostToolUse response read from stdout on exit 0.
type hookOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// runHook checks the file named in a PostToolUse event. Claude Code
// shows the model only stderr on exit 2 and additionalContext on
// exit 0, so errors go to stderr and the rest into the JSON response.
func runHook(_ []string) int {
	return hook(os.Stdin, os.Stdout, os.Stderr)
}

func hook(in io.Reader, out, errw io.Writer) int {
	var ev hookInput
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		_, _ = fmt.Fprintln(errw, "gochecks hook: bad input:", err)
		return 1
	}
	switch ev.HookEventName {
	case "SessionStart":
		return hookSessionStart(ev, out)
	case "Stop":
		return hookStop(ev, errw)
	case "UserPromptSubmit":
		return hookPrompt(ev, out)
	}
	if ev.ToolInput.FilePath == "" {
		return 0
	}
	invocationNote = ev.ToolInput.FilePath

	files, err := resolveChanged([]string{ev.ToolInput.FilePath})
	if err != nil {
		_, _ = fmt.Fprintln(errw, "gochecks hook:", err)
		return 1
	}
	if len(files) == 0 {
		return 0
	}
	cfg, err := loadConfig(filepath.Dir(files[0]))
	if err != nil {
		_, _ = fmt.Fprintln(errw, "gochecks hook: config:", err)
		return 1
	}
	files = cfg.applyExclude(files)
	if len(files) == 0 {
		return 0
	}

	findings, skipped, err := check(files, false, cfg)
	if err != nil {
		_, _ = fmt.Fprintln(errw, "gochecks hook:", err)
		return 1
	}
	invocationFindings = len(findings)
	note := skippedNote(skipped, ev.CWD, cfg)
	if len(findings) == 0 {
		// The package does not compile, but not in this file: say so
		// rather than read as clean.
		if note != "" {
			writeContext(out, "PostToolUse", "gochecks: "+note)
		}
		return 0
	}

	report := hookReport(findings, ev.CWD) + note
	if exitFor(findings) == 2 {
		_, _ = fmt.Fprint(errw, report)
		return 2
	}
	var resp hookOutput
	resp.HookSpecificOutput.HookEventName = "PostToolUse"
	resp.HookSpecificOutput.AdditionalContext = report
	writeJSON(out, resp)
	return 0
}

// relPath is path relative to cwd when it is inside cwd, else path.
func relPath(path, cwd string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

// hookReport renders findings one per line, paths relative to cwd.
func hookReport(findings []Finding, cwd string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gochecks: %d finding(s)\n", len(findings))
	for _, f := range findings {
		path := relPath(f.File, cwd)
		if f.Line == 0 {
			// A file-level finding names the file; a package-level
			// one, such as a broken tool, names the directory.
			if fi, err := os.Stat(f.File); err == nil && fi.IsDir() {
				path = strings.TrimSuffix(path, "/") + "/"
			}
			fmt.Fprintf(&b, "%s %s/%s: %s\n", path, f.Class, f.Tool, f.Msg)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d %s/%s: %s\n", path, f.Line, f.Col, f.Class, f.Tool, f.Msg)
	}
	return b.String()
}

// hookSessionStart adds, once, what gochecks runs for the agent where
// the Stop hook is wired, findings a session in this directory left
// standing, a count of what audit lists, and dependencies that are
// behind. Anything that goes wrong stays silent; a session start is not
// the place to report network trouble.
func hookSessionStart(ev hookInput, out io.Writer) int {
	dir := ev.CWD
	if dir == "" {
		return 0
	}
	text := coverageNote(dir) + unresolvedIn(dir) + auditSummary(dir)
	if modulePath(dir) != "" {
		if cfg, err := loadConfig(dir); err == nil {
			if deps, err := behindDeps(dir, cfg); err == nil && len(deps) > 0 {
				invocationFindings = len(deps)
				text += depsSummary(deps)
			}
		}
	}
	if text != "" {
		writeContext(out, "SessionStart", text)
	}
	return 0
}

// writeJSON encodes without HTML escaping so "->" stays readable.
func writeJSON(out io.Writer, v any) {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
