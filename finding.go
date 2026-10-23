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
}

func emit(findings []Finding, textOut bool) {
	if textOut {
		// Same line form as the hook report, with absolute paths.
		fmt.Print(strings.TrimPrefix(hookReport(findings, ""), fmt.Sprintf("gochecks: %d finding(s)\n", len(findings))))
		return
	}
	enc := json.NewEncoder(os.Stdout)
	for _, f := range findings {
		_ = enc.Encode(f)
	}
}
