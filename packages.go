package main

import "path/filepath"

func groupByPackage(files []string) map[string][]string {
	out := map[string][]string{}
	for _, f := range files {
		dir := filepath.Dir(f)
		out[dir] = append(out[dir], f)
	}
	return out
}
