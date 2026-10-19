package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func runShow(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var context int
	fs.IntVar(&context, "context", 3, "source lines of context on each side")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "gochecks show: expected <file:line>")
		return 2
	}

	file, line, err := parseFileLine(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks show:", err)
		return 2
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks show:", err)
		return 2
	}

	printExcerpt(abs, line, context)

	pkgDir := filepath.Dir(abs)
	findings, err := runVet(pkgDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\ngochecks show: vet:", err)
		return 2
	}
	var matched []Finding
	for _, f := range findings {
		if f.File != abs {
			continue
		}
		if line != 0 && f.Line != line {
			continue
		}
		matched = append(matched, f)
	}

	if len(matched) == 0 {
		fmt.Printf("\n(no vet findings at %s:%d)\n", abs, line)
		return 0
	}
	for _, f := range matched {
		fmt.Printf("\n%s:%d:%d [%s/%s] %s\n", f.File, f.Line, f.Col, f.Class, f.Tool, f.Msg)
	}
	return 0
}

func parseFileLine(s string) (string, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("expected <file:line>, got %q", s)
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid line number in %q", s)
	}
	return parts[0], line, nil
}

func printExcerpt(file string, line, ctx int) {
	f, err := os.Open(file)
	if err != nil {
		fmt.Printf("(cannot read %s: %v)\n", file, err)
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	start := max(line-ctx, 1)
	end := line + ctx
	n := 0
	for sc.Scan() {
		n++
		if n < start {
			continue
		}
		if n > end {
			break
		}
		marker := "  "
		if n == line {
			marker = "> "
		}
		fmt.Printf("%s%4d | %s\n", marker, n, sc.Text())
	}
}
