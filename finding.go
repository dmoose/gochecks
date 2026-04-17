package main

import (
	"encoding/json"
	"fmt"
	"os"
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
		for _, f := range findings {
			fmt.Printf("%s:%d:%d:%s:%s: %s\n", f.File, f.Line, f.Col, f.Class, f.Tool, f.Msg)
		}
		return
	}
	enc := json.NewEncoder(os.Stdout)
	for _, f := range findings {
		_ = enc.Encode(f)
	}
}
