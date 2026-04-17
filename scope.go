package main

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func resolveChanged(in []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range in {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if fi.IsDir() {
			continue
		}
		if !strings.HasSuffix(abs, ".go") {
			continue
		}
		if isVendored(abs) {
			continue
		}
		gen, err := isGenerated(abs)
		if err != nil {
			return nil, err
		}
		if gen {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out, nil
}

func isVendored(abs string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(abs), "/") {
		if seg == "vendor" {
			return true
		}
	}
	return false
}

var generatedRE = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

func isGenerated(abs string) (bool, error) {
	f, err := os.Open(abs)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if generatedRE.MatchString(line) {
			return true, nil
		}
		if strings.HasPrefix(line, "package ") {
			return false, nil
		}
	}
	return false, sc.Err()
}
