package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// sessionState is what the Stop hook remembers about one session.
type sessionState struct {
	CWD string `json:"cwd"`
	// LastBlock hashes the blocking set of the last block; "" once a
	// stop passes.
	LastBlock string    `json:"last_block,omitempty"`
	Count     int       `json:"count,omitempty"`
	BlockedAt time.Time `json:"blocked_at"`
	// Unresolved is set when a stuck session was let through with
	// findings standing. Every prompt and the next session in the same
	// directory are reminded until a stop passes.
	Unresolved bool `json:"unresolved,omitempty"`
	// Pending holds suggestions from the last stop, for the next
	// prompt: context added at Stop would keep the turn going.
	Pending string `json:"pending,omitempty"`
}

// allowGrant is a one-shot pass for one session and blocking set.
type allowGrant struct {
	Hash   string    `json:"hash"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func stopDir(sub string) (string, error) {
	base, err := cacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "stop", sub)
	return dir, os.MkdirAll(dir, 0o755)
}

// sessionFile names id's file under sub, refusing ids that are not a
// plain name.
func sessionFile(sub, id string) (string, error) {
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return "", fmt.Errorf("bad session id %q", id)
	}
	dir, err := stopDir(sub)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644) //nolint:gosec // paths come from sessionFile, which refuses ids that are not a plain name
}

func loadSession(id string) (sessionState, string) {
	var st sessionState
	path, err := sessionFile("sessions", id)
	if err != nil {
		return st, ""
	}
	_ = readJSON(path, &st)
	return st, path
}

// blockHash identifies a blocking set: the same findings, wherever
// they moved to by line, hash the same only if nothing about them
// changed.
func blockHash(fs []Finding) string {
	lines := make([]string, len(fs))
	for i, f := range fs {
		lines[i] = fmt.Sprintf("%s\t%d\t%d\t%s\t%s\t%s", f.File, f.Line, f.Col, f.Class, f.Tool, f.Msg)
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
}

// stopFindings runs check --no-apply on the changed Go files of the
// repos a Stop run covers, one run per config.
func stopFindings(cwd string) ([]Finding, error) {
	var raw []string
	for _, repo := range stopRepos(cwd) {
		raw = append(raw, changedGoFiles(repo)...)
	}
	files, err := resolveChanged(raw)
	if err != nil {
		return nil, err
	}
	groups := map[string][]string{}
	cfgs := map[string]Config{}
	for _, f := range files {
		cfg, err := loadConfig(filepath.Dir(f))
		if err != nil {
			return nil, err
		}
		groups[cfg.Path] = append(groups[cfg.Path], f)
		cfgs[cfg.Path] = cfg
	}
	var all []Finding
	for _, key := range slices.Sorted(maps.Keys(groups)) {
		cfg := cfgs[key]
		group := cfg.applyExclude(groups[key])
		if len(group) == 0 {
			continue
		}
		fs, err := check(group, true, cfg)
		if err != nil {
			return nil, err
		}
		all = append(all, fs...)
	}
	return all, nil
}

const stopGuidance = `Fix these before stopping. Touching a file means owning every finding
in it, whoever wrote the line: "not mine" only means "not in my context".
If a finding looks wrong, do not silence it; stop and ask the user, who
can release this stop with: gochecks allow "<reason>"
`

// hookStop blocks the end of a turn while files changed since HEAD
// have errors or warnings. See HOOKS.md for the protocol.
func hookStop(ev hookInput, errw io.Writer) int {
	if ev.CWD == "" {
		return 0
	}
	// Without git there are no changed files to see; say so rather
	// than pass.
	if _, err := exec.LookPath("git"); err != nil {
		_, _ = fmt.Fprintln(errw, "gochecks hook: git not on PATH; the Stop check cannot see changed files")
		return 1
	}
	findings, err := stopFindings(ev.CWD)
	if err != nil {
		_, _ = fmt.Fprintln(errw, "gochecks hook:", err)
		return 1
	}
	invocationFindings = len(findings)
	var blocking, rest []Finding
	for _, f := range findings {
		if f.Class == "error" || f.Class == "warning" {
			blocking = append(blocking, f)
		} else {
			rest = append(rest, f)
		}
	}

	st, path := loadSession(ev.SessionID)
	st.CWD = ev.CWD
	st.Pending = ""
	if len(rest) > 0 {
		st.Pending = hookReport(rest, ev.CWD)
	}
	save := func() {
		if path != "" {
			_ = writeJSONFile(path, st)
		}
	}

	if len(blocking) == 0 {
		st.LastBlock, st.Count, st.Unresolved = "", 0, false
		save()
		return 0
	}
	h := blockHash(blocking)
	if g, ok := takeAllow(ev.SessionID, h); ok {
		invocationNote = "allowed: " + g.Reason
		st.LastBlock, st.Count, st.Unresolved = "", 0, false
		save()
		return 0
	}
	if ev.StopHookActive && st.LastBlock == h {
		// The agent made no progress since the last block: let the turn
		// end rather than loop, and remind until a stop passes.
		invocationNote = "stuck: let through, unresolved"
		st.Unresolved = true
		save()
		return 0
	}
	st.LastBlock, st.Count, st.BlockedAt = h, len(blocking), time.Now().UTC()
	save()
	_, _ = fmt.Fprint(errw, strings.Replace(hookReport(blocking, ev.CWD), "finding(s)", "blocking finding(s) in files changed since HEAD", 1))
	_, _ = fmt.Fprint(errw, stopGuidance)
	return 2
}

// takeAllow consumes a grant for this session when it matches hash.
func takeAllow(id, hash string) (allowGrant, bool) {
	var g allowGrant
	path, err := sessionFile("allows", id)
	if err != nil || readJSON(path, &g) != nil || g.Hash != hash {
		return g, false
	}
	_ = os.Remove(path)
	appendAllowLog(fmt.Sprintf("used session=%s hash=%s", id, hash))
	return g, true
}

const unresolvedLine = "gochecks: %d blocking finding(s) from the last stop still stand in %s. Fix them, or ask the user to run: gochecks allow \"<reason>\"\n"

// hookPrompt reminds of findings left standing and passes on
// suggestions from the last stop.
func hookPrompt(ev hookInput, out io.Writer) int {
	st, path := loadSession(ev.SessionID)
	var b strings.Builder
	if st.Unresolved {
		fmt.Fprintf(&b, unresolvedLine, st.Count, st.CWD)
	}
	if st.Pending != "" {
		b.WriteString(st.Pending)
		st.Pending = ""
		if path != "" {
			_ = writeJSONFile(path, st)
		}
	}
	if b.Len() == 0 {
		return 0
	}
	writeContext(out, "UserPromptSubmit", b.String())
	return 0
}

// unresolvedIn returns the reminder for a session that ended in dir
// with findings standing, or "".
func unresolvedIn(dir string) string {
	sdir, err := stopDir("sessions")
	if err != nil {
		return ""
	}
	entries, _ := os.ReadDir(sdir)
	for _, e := range entries {
		var st sessionState
		if readJSON(filepath.Join(sdir, e.Name()), &st) == nil && st.Unresolved && st.CWD == dir {
			return fmt.Sprintf(unresolvedLine, st.Count, st.CWD)
		}
	}
	return ""
}

func writeContext(out io.Writer, event, text string) {
	var resp hookOutput
	resp.HookSpecificOutput.HookEventName = event
	resp.HookSpecificOutput.AdditionalContext = text
	writeJSON(out, resp)
}

func appendAllowLog(line string) {
	dir, err := stopDir("")
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "allow.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
}

var errNoBlock = errors.New("no blocked session")

// blockedSession picks the session to release: id when given, else the
// most recent block in dir or below it.
func blockedSession(id, dir string) (string, sessionState, error) {
	if id != "" {
		st, path := loadSession(id)
		if path == "" || st.LastBlock == "" {
			return "", st, fmt.Errorf("%w: %s", errNoBlock, id)
		}
		return id, st, nil
	}
	sdir, err := stopDir("sessions")
	if err != nil {
		return "", sessionState{}, err
	}
	entries, _ := os.ReadDir(sdir)
	var best string
	var bestSt sessionState
	for _, e := range entries {
		var st sessionState
		if readJSON(filepath.Join(sdir, e.Name()), &st) != nil || st.LastBlock == "" {
			continue
		}
		if rel, err := filepath.Rel(dir, st.CWD); err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if best == "" || st.BlockedAt.After(bestSt.BlockedAt) {
			best, bestSt = strings.TrimSuffix(e.Name(), ".json"), st
		}
	}
	if best == "" {
		return "", bestSt, fmt.Errorf("%w in %s", errNoBlock, dir)
	}
	return best, bestSt, nil
}

// runAllow grants one pass to a blocked session. It is for the user:
// install --stop denies it to the agent.
func runAllow(args []string) int {
	var id string
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--session" && i+1 < len(args):
			id = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--session="):
			id = strings.TrimPrefix(args[i], "--session=")
		default:
			rest = append(rest, args[i])
		}
	}
	reason := strings.TrimSpace(strings.Join(rest, " "))
	if reason == "" {
		fmt.Fprintln(os.Stderr, `gochecks allow: a reason is required: gochecks allow [--session id] "<reason>"`)
		return 2
	}
	cwd, _ := os.Getwd()
	id, st, err := blockedSession(id, cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks allow:", err)
		return 2
	}
	path, err := sessionFile("allows", id)
	if err == nil {
		err = writeJSONFile(path, allowGrant{Hash: st.LastBlock, Reason: reason, At: time.Now().UTC()})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks allow:", err)
		return 2
	}
	if st.Unresolved {
		st.Unresolved = false
		if spath, err := sessionFile("sessions", id); err == nil {
			_ = writeJSONFile(spath, st)
		}
	}
	appendAllowLog(fmt.Sprintf("granted session=%s hash=%s cwd=%s findings=%d reason=%q", id, st.LastBlock, st.CWD, st.Count, reason))
	invocationNote = "session=" + id
	fmt.Printf("gochecks: allowed one stop past %d finding(s) in %s (session %s)\n", st.Count, st.CWD, id)
	return 0
}
