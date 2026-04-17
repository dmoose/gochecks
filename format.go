package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var errGoimportsMissing = errors.New("goimports binary not found on PATH")

func runGofmt(files []string, apply bool) ([]Finding, error) {
	mode := "-l"
	if apply {
		mode = "-w"
	}
	args := append([]string{"-s", mode}, files...)
	cmd := exec.Command("gofmt", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if apply {
		return nil, nil
	}
	return linesToFindings(stdout.String(), "gofmt", "needs gofmt -s"), nil
}

func runGoimports(files []string, apply bool) ([]Finding, error) {
	if _, err := exec.LookPath("goimports"); err != nil {
		return nil, errGoimportsMissing
	}
	mode := "-l"
	if apply {
		mode = "-w"
	}
	args := append([]string{mode}, files...)
	cmd := exec.Command("goimports", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if apply {
		return nil, nil
	}
	return linesToFindings(stdout.String(), "goimports", "needs goimports"), nil
}

func linesToFindings(out, tool, msg string) []Finding {
	var fs []Finding
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fs = append(fs, Finding{
			File:  line,
			Class: "suggestion",
			Tool:  tool,
			Msg:   msg,
		})
	}
	return fs
}
