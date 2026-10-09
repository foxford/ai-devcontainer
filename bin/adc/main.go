package main

import (
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
)

func main() {
	args := os.Args[1:]
	name := filepath.Base(os.Args[0])
	if os.Getenv("ADC_RUNTIME") == "1" && shared.Contains([]string{"claude", "codex", "opencode", "hermes", "dsh"}, name) {
		args = append([]string{"agent", name, "--"}, args...)
	}
	if err := Run(args); err != nil {
		fmt.Fprintln(os.Stderr, "adc:", err)
		os.Exit(1)
	}
}
