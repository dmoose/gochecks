package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// hookInput is the subset of the Claude Code PostToolUse payload we
// read. Hooks receive the full event as JSON on stdin.
type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name"`
	CWD           string `json:"cwd"`
	ToolInput     struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// hookOutput is the PostToolUse response shape Claude Code reads from
// stdout on exit 0. additionalContext lands next to the tool result.
type hookOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// runHook is the PostToolUse entry point. It reads the event from
// stdin, checks the edited file if it is Go, and reports back through
// the two channels Claude Code actually surfaces to the model:
//
//   - error-class findings: text on stderr, exit 2
//   - warnings and suggestions: additionalContext JSON on stdout, exit 0
//   - nothing to say: no output, exit 0
//
// Plain stdout on exit 0 and stderr on exit 1 never reach the model,
// so `check` output is not reused here.
func runHook(args []string) int {
	return hook(os.Stdin, os.Stdout, os.Stderr)
}

func hook(in io.Reader, out, errw io.Writer) int {
	var ev hookInput
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		fmt.Fprintln(errw, "gocheck hook: bad input:", err)
		return 1
	}
	if ev.ToolInput.FilePath == "" {
		return 0
	}
	invocationNote = ev.ToolInput.FilePath

	files, err := resolveChanged([]string{ev.ToolInput.FilePath})
	if err != nil {
		fmt.Fprintln(errw, "gocheck hook:", err)
		return 1
	}
	if len(files) == 0 {
		return 0
	}
	cfg, err := loadConfig(filepath.Dir(files[0]))
	if err != nil {
		fmt.Fprintln(errw, "gocheck hook: config:", err)
		return 1
	}
	files = cfg.applyExclude(files)
	if len(files) == 0 {
		return 0
	}

	findings, err := check(files, false, cfg)
	if err != nil {
		fmt.Fprintln(errw, "gocheck hook:", err)
		return 1
	}
	invocationFindings = len(findings)
	if len(findings) == 0 {
		return 0
	}

	report := hookReport(findings, ev.CWD)
	if exitFor(findings) == 2 {
		fmt.Fprint(errw, report)
		return 2
	}
	var resp hookOutput
	resp.HookSpecificOutput.HookEventName = "PostToolUse"
	resp.HookSpecificOutput.AdditionalContext = report
	_ = json.NewEncoder(out).Encode(resp)
	return 0
}

// hookReport renders findings for the model: one line each, paths
// relative to the session cwd to keep the token count down.
func hookReport(findings []Finding, cwd string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gocheck: %d finding(s)\n", len(findings))
	for _, f := range findings {
		path := f.File
		if cwd != "" {
			if rel, err := filepath.Rel(cwd, f.File); err == nil && !strings.HasPrefix(rel, "..") {
				path = rel
			}
		}
		if f.Line == 0 {
			// Package-level finding (modernize conflicts, test build
			// failures): the "file" is the package directory.
			fmt.Fprintf(&b, "%s/ %s/%s: %s\n", strings.TrimSuffix(path, "/"), f.Class, f.Tool, f.Msg)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d %s/%s: %s\n", path, f.Line, f.Col, f.Class, f.Tool, f.Msg)
	}
	return b.String()
}
