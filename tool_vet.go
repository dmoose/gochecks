package main

import "strings"

// runVet runs go vet -json. A package that fails to typecheck reports
// plain text on stderr instead of JSON.
func runVet(pkgDir string) ([]Finding, error) {
	stdout, stderr := runTool(pkgDir, "go", "vet", "-json", ".")
	fs := parseVet(stdout, pkgDir)
	return append(fs, parseVetText(stderr, pkgDir)...), nil
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
