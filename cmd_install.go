package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed internal/install/go-current.SKILL.md
var goCurrentSkill string

//go:embed internal/install/gochecks.md
var gochecksDoc string

// hookCommand is the PostToolUse command written to .claude/settings.json.
// The hook reads the event from stdin and skips non-Go files itself.
const hookCommand = `gochecks hook`

// hookMatcher names the file-editing tools.
const hookMatcher = "Edit|Write"

func runInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		dir     string
		force   bool
		upgrade bool
		noHook  bool
		noSkill bool
		noDoc   bool
		config  bool
	)
	fs.StringVar(&dir, "dir", ".", "target repo root")
	fs.BoolVar(&force, "force", false, "overwrite existing files")
	fs.BoolVar(&upgrade, "upgrade", false, "refresh gochecks-owned files (skill, hook block, doc) in place")
	fs.BoolVar(&noHook, "no-hook", false, "skip .claude/settings.json wiring")
	fs.BoolVar(&noSkill, "no-skill", false, "skip .claude/skills/go-current installation")
	fs.BoolVar(&noDoc, "no-doc", false, "skip .claude/gochecks.md doc drop")
	fs.BoolVar(&config, "config", false, "also scaffold a default .gochecks.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks: install:", err)
		return 2
	}

	if !noHook {
		if err := installHook(root, force || upgrade); err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: install hook:", err)
			return 2
		}
	}
	if !noSkill {
		if err := installSkill(root, force || upgrade); err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: install skill:", err)
			return 2
		}
	}
	if !noDoc {
		if err := installDoc(root, force || upgrade); err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: install doc:", err)
			return 2
		}
	}
	if config {
		if err := installConfig(root, force); err != nil {
			fmt.Fprintln(os.Stderr, "gochecks: install config:", err)
			return 2
		}
	}
	fmt.Println("gochecks: installed into", root)
	return 0
}

func installHook(root string, overwrite bool) error {
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	existing := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("%s: %w (rerun with --force to overwrite)", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	hook := map[string]any{
		"matcher": hookMatcher,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hookCommand,
			},
		},
	}

	hooks, _ := existing["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	post, _ := hooks["PostToolUse"].([]any)

	// Replace an existing entry only with --force or --upgrade.
	idx := findGocheckHook(post)
	if idx >= 0 {
		if !overwrite {
			fmt.Printf("gochecks: hook already present in %s (use --force or --upgrade to replace)\n", settingsPath)
			return nil
		}
		post[idx] = hook
	} else {
		post = append(post, hook)
	}
	hooks["PostToolUse"] = post
	existing["hooks"] = hooks

	buf, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(buf, '\n'), 0o644)
}

func findGocheckHook(entries []any) int {
	for i, e := range entries {
		m, _ := e.(map[string]any)
		hooks, _ := m["hooks"].([]any)
		for _, h := range hooks {
			hm, _ := h.(map[string]any)
			cmd, _ := hm["command"].(string)
			if cmd != "" && containsGocheck(cmd) {
				return i
			}
		}
	}
	return -1
}

func containsGocheck(s string) bool {
	// gocheck is the pre-rename spelling written by older installs.
	for _, sub := range []string{"gochecks hook", "gochecks check", "gochecks fix", "gocheck hook", "gocheck check", "gocheck fix"} {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func installSkill(root string, overwrite bool) error {
	skillDir := filepath.Join(root, ".claude", "skills", "go-current")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(path); err == nil && !overwrite {
		fmt.Printf("gochecks: skill already present at %s (use --force or --upgrade to replace)\n", path)
		return nil
	}
	return os.WriteFile(path, []byte(goCurrentSkill), 0o644)
}

func installDoc(root string, overwrite bool) error {
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "gochecks.md")
	if _, err := os.Stat(path); err == nil && !overwrite {
		fmt.Printf("gochecks: doc already present at %s (use --force or --upgrade to replace)\n", path)
		return nil
	}
	return os.WriteFile(path, []byte(gochecksDoc), 0o644)
}

func installConfig(root string, overwrite bool) error {
	path := filepath.Join(root, configFilename)
	if _, err := os.Stat(path); err == nil && !overwrite {
		fmt.Printf("gochecks: %s already present (use --force to replace)\n", path)
		return nil
	}
	cfg := defaultConfig()
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(buf, '\n'), 0o644)
}
