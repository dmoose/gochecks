package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// ynode is one line of a YAML file as the small reader below sees it:
// the dotted key path it sits under, its value, and whether it is a
// list item. It reads the subset golangci-lint configs use: nested
// maps, block and inline lists. It is not a YAML parser.
type ynode struct {
	path   string
	val    string
	line   int
	indent int
	item   bool
}

func readYAML(text string) []ynode {
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	var nodes []ynode
	path := func() string {
		keys := make([]string, len(stack))
		for i, f := range stack {
			keys[i] = f.key
		}
		return strings.Join(keys, ".")
	}
	for i, raw := range strings.Split(text, "\n") {
		code := stripYAMLComment(raw)
		trimmed := strings.TrimSpace(code)
		if trimmed == "" || trimmed == "---" {
			continue
		}
		indent := len(code) - len(strings.TrimLeft(code, " "))
		item := strings.HasPrefix(trimmed, "- ") || trimmed == "-"
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if top.indent > indent || (top.indent == indent && !item) {
				stack = stack[:len(stack)-1]
				continue
			}
			break
		}
		content := trimmed
		if item {
			content = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			nodes = append(nodes, ynode{path: path(), val: content, line: i + 1, indent: indent, item: true})
			key, val, ok := strings.Cut(content, ":")
			if !ok || strings.ContainsAny(key, " \"'") {
				continue
			}
			// "- key: val" opens a map inside the item.
			if strings.TrimSpace(val) == "" {
				stack = append(stack, frame{indent + 2, key})
			}
			continue
		}
		key, val, ok := strings.Cut(content, ":")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		p := key
		if pp := path(); pp != "" {
			p = pp + "." + key
		}
		nodes = append(nodes, ynode{path: p, val: val, line: i + 1, indent: indent})
		if val == "" {
			stack = append(stack, frame{indent, key})
		}
	}
	return nodes
}

func stripYAMLComment(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	code, _, _ := strings.Cut(line, " #")
	return code
}

func yScalar(nodes []ynode, path string) string {
	for _, n := range nodes {
		if !n.item && n.path == path {
			return strings.Trim(n.val, `"'`)
		}
	}
	return ""
}

// yList returns the items of a block or inline list.
func yList(nodes []ynode, path string) []string {
	var out []string
	for _, n := range nodes {
		switch {
		case n.item && n.path == path:
			out = append(out, strings.Trim(n.val, `"'`))
		case !n.item && n.path == path && strings.HasPrefix(n.val, "["):
			for v := range strings.SplitSeq(strings.Trim(n.val, "[]"), ",") {
				if v = strings.Trim(strings.TrimSpace(v), `"'`); v != "" {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// yItems returns the list items at path with the text of each one's
// block on one line.
func yItems(nodes []ynode, path string) []ynode {
	var out []ynode
	for i, n := range nodes {
		if !n.item || n.path != path {
			continue
		}
		// "key: val" parts; items under a key join its value.
		parts := []string{n.val}
		for _, m := range nodes[i+1:] {
			if m.indent <= n.indent {
				break
			}
			last := &parts[len(parts)-1]
			switch {
			case m.item && strings.HasSuffix(*last, ": "):
				*last += m.val
			case m.item:
				*last += ", " + m.val
			default:
				parts = append(parts, m.path[strings.LastIndex(m.path, ".")+1:]+": "+m.val)
			}
		}
		n.val = strings.Join(parts, "; ")
		out = append(out, n)
	}
	return out
}

var standardLinters = []string{"errcheck", "govet", "ineffassign", "staticcheck", "unused"}

// enabledLinters is the v2 set a config turns on, and whether it could
// be worked out.
func enabledLinters(nodes []ynode) ([]string, bool) {
	enable := yList(nodes, "linters.enable")
	switch yScalar(nodes, "linters.default") {
	case "none":
		return enable, true
	case "", "standard":
		return append(slices.Clone(standardLinters), enable...), true
	}
	return nil, false // all or fast: everything not disabled
}

// auditGolangci lists how a repo's golangci-lint config departs from
// the gochecks profile: profile linters it does not run, linters it
// disables, exclusions and severity overrides.
func auditGolangci(path string) []auditItem {
	data, err := os.ReadFile(path)
	if err != nil || string(data) == lintProfile {
		return nil
	}
	items := []auditItem{{Kind: kindGolangci, File: path, Msg: "differs from the gochecks lint profile (gochecks lint-config prints it)"}}
	add := func(line int, msg string) {
		items = append(items, auditItem{Kind: kindGolangci, File: path, Line: line, Msg: msg})
	}
	if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
		return items
	}
	nodes := readYAML(string(data))
	profile := readYAML(lintProfile)
	if v := yScalar(nodes, "version"); v != "2" {
		add(0, "not a version 2 config: golangci-lint v2 does not read it as written")
		return items
	}

	want, _ := enabledLinters(profile)
	if have, ok := enabledLinters(nodes); ok {
		var missing []string
		for _, l := range want {
			if !slices.Contains(have, l) {
				missing = append(missing, l)
			}
		}
		if len(missing) > 0 {
			add(0, fmt.Sprintf("does not enable %d profile linter(s): %s", len(missing), strings.Join(missing, ", ")))
		}
	}
	for _, n := range yItems(nodes, "linters.disable") {
		add(n.line, "disables "+n.val)
	}
	presets := yList(profile, "linters.exclusions.presets")
	for _, n := range yItems(nodes, "linters.exclusions.presets") {
		if !slices.Contains(presets, n.val) {
			add(n.line, "exclusion preset "+n.val)
		}
	}
	for _, sub := range []string{"rules", "paths", "paths-except"} {
		for _, n := range yItems(nodes, "linters.exclusions."+sub) {
			add(n.line, "exclusion "+strings.TrimSuffix(sub, "s")+": "+n.val)
		}
	}
	for _, n := range nodes {
		if strings.HasPrefix(n.path, "issues.") && !n.item && (strings.Contains(n.path, "exclude") || n.path == "issues.max-issues-per-linter" || n.path == "issues.max-same-issues") {
			add(n.line, n.path[len("issues."):]+": "+n.val)
		}
	}
	for _, n := range nodes {
		if n.path == "severity" {
			add(n.line, "severity overrides")
			break
		}
	}
	return items
}
