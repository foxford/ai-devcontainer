package runtime

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func checkoutRoot(dir string) string {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func Banner(dir, workspace string, out io.Writer) error {
	if workspace == "" {
		workspace = "developer"
	}
	fmt.Fprintf(out, "🤖 ADC · %s\n\n", workspace)
	fmt.Fprintln(out, "  Агенты     claude · codex · opencode · hermes · dsh\n  Проекты    adc project list · adc workspace map\n  Контекст   adc sync · adc doctor\n  Скиллы     adc skill list · adc skill status\n  MCP        adc mcp list · adc mcp enable <имя>\n  Задачи     adc task --help · adc plans --help\n  Инструменты adc tools --help · adc omniroute --help")
	root := checkoutRoot(dir)
	if root != "" {
		hook := filepath.Join(root, ".agents", "motd.sh")
		if info, err := os.Stat(hook); err == nil && info.Mode().IsRegular() {
			fmt.Fprintln(out)
			// This is a project-owned extension, deliberately executed by its shell.
			cmd := exec.Command("sh", hook)
			cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, os.Stdin, out, out
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(out, "Подсказки проекта: %v\n", err)
			}
		}
	}
	fmt.Fprintln(out)
	return nil
}
