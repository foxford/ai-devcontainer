package runtime

import (
	"errors"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) Agent(args []string) error {
	if len(args) == 0 {
		return errors.New("adc agent <claude|codex|opencode|hermes|dsh> [arguments...]")
	}
	name, argv := args[0], args[1:]
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if !shared.Contains([]string{"claude", "codex", "opencode", "hermes", "dsh"}, name) {
		return errors.New("неподдерживаемый agent adapter")
	}
	real := filepath.Join("/opt/ai-tools/bin", name)
	if _, err := os.Stat(real); err != nil {
		return err
	}
	administrative := os.Getenv("ADC_DELIVERY") == "1"
	for _, arg := range argv {
		if arg == "--version" || arg == "--help" || arg == "-h" {
			administrative = true
		}
	}
	if len(argv) > 0 && shared.Contains([]string{"mcp", "login", "logout", "auth", "skills", "setup", "config"}, argv[0]) {
		administrative = true
	}
	if !administrative {
		dir, _ := os.Getwd()
		root, err := shared.Output(dir, "git", "rev-parse", "--show-toplevel")
		if err == nil {
			// Context arrives before the real agent reads its instructions, including
			// worktrees created through the Orca UI with an uncommitted setup config.
			previous := os.Stdout
			os.Stdout = os.Stderr
			err = a.Sync([]string{strings.TrimSpace(root)})
			os.Stdout = previous
			if err != nil {
				return err
			}
		}
	}
	return shared.Run("", real, argv...)
}
