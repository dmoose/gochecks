package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Finding struct {
	File  string `json:"file"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Class string `json:"class"`
	Tool  string `json:"tool"`
	Msg   string `json:"msg"`
	// broken marks a tool that cannot read this toolchain; such a
	// finding is not tied to the changed files.
	broken bool
	// failed marks a tool run that loaded nothing: it names the
	// package or module directory, not a changed file.
	failed bool
}

func emit(findings []Finding, textOut bool) {
	if textOut {
		// Same line form as the hook report, paths relative to the
		// working directory.
		cwd, _ := os.Getwd()
		fmt.Print(strings.TrimPrefix(hookReport(findings, cwd), fmt.Sprintf("gochecks: %d finding(s)\n", len(findings))))
		return
	}
	enc := json.NewEncoder(os.Stdout)
	for _, f := range findings {
		_ = enc.Encode(f)
	}
}
