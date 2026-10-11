package main

import (
	"fmt"
	runtimecmd "github.com/foxford/ai-devcontainer/bin/adc/runtime"
	"os"
	"path/filepath"
)

func main() {
	args := os.Args[1:]
	name := filepath.Base(os.Args[0])
	if os.Getenv("ADC_RUNTIME") == "1" && runtimecmd.IsAgent(name) {
		args = append([]string{"agent", name, "--"}, args...)
	}
	if err := Run(args); err != nil {
		fmt.Fprintln(os.Stderr, "adc:", err)
		os.Exit(1)
	}
}
