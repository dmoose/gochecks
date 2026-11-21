package main

import "strings"

// runVet runs go vet -json. A package that fails to typecheck reports
// plain text on stderr instead of JSON.
func runVet(pkgDir string) ([]Finding, error) {
	r := runTool(pkgDir, "go", "vet", "-json", ".")
	fs := append(parseVet(r.stdout, pkgDir), parseVetText(r.stderr, pkgDir)...)
	// -json exits 0 with diagnostics; non-zero means vet did not run.
	if len(fs) == 0 {
		return failedRun(r, pkgDir, "vet", "warning", r.stderr), nil
	}
	return fs, nil
}

func parseVet(out, pkgDir string) []Finding {
	return analysisFindings(parseAnalysisJSON(out), pkgDir, "vet", "error", func(_, msg string) string { return msg })
}

// parseVetText handles "vet: ./x.go:3:2: msg" typecheck lines.
func parseVetText(out, pkgDir string) []Finding {
	var b strings.Builder
	for line := range strings.SplitSeq(out, "\n") {
		b.WriteString(strings.TrimPrefix(strings.TrimSpace(line), "vet: "))
		b.WriteByte('\n')
	}
	return textFindings(b.String(), pkgDir, "vet", "error")
}
