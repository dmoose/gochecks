package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// runStatus prints the loaded config and the hook log tail. Tool
// health is `gochecks doctor`.
func runStatus(args []string) int {
	const tailN = 20

	dir, err := cacheDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gochecks: cacheDir:", err)
		return 2
	}

	fmt.Println("gochecks", version)
	fmt.Println()

	fmt.Println("state dir:")
	fmt.Println("  path:", dir)
	fmt.Println()

	cwd, _ := os.Getwd()
	cfg, cfgErr := loadConfig(cwd)
	fmt.Println("config:")
	if cfgErr != nil {
		fmt.Printf("  error:  %v\n", cfgErr)
	} else if cfg.Path == "" {
		fmt.Println("  (defaults — no .gochecks.json found walking up from cwd)")
	} else {
		fmt.Printf("  file:   %s\n", cfg.Path)
	}
	fmt.Printf("  test:         %t\n", cfg.Test)
	fmt.Printf("  test_timeout: %s\n", cfg.testTimeout())
	fmt.Printf("  auto_fix:     %t\n", cfg.AutoFix)
	fmt.Printf("  linters:      %t\n", cfg.Linters)
	fmt.Printf("  exclude:      %v\n", cfg.Exclude)
	fmt.Println()

	logPath := filepath.Join(dir, "hook.log")
	fmt.Printf("last %d hook invocations (%s):\n", tailN, logPath)
	lines, err := tailLines(logPath, tailN)
	if err != nil {
		fmt.Println("  (no log yet)")
	} else {
		for _, l := range lines {
			fmt.Println("  " + l)
		}
	}
	_ = args
	return 0
}

// tailLines returns the last n lines of path, in file order.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if len(buf) == n {
			buf = buf[1:]
		}
		buf = append(buf, sc.Text())
	}
	return buf, sc.Err()
}
