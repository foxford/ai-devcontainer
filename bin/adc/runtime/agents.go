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
	driver := findAgentDriver(name)
	if driver == nil {
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
	dir, _ := os.Getwd()
	root, gitErr := shared.Output(dir, "git", "rev-parse", "--show-toplevel")
	if gitErr == nil {
		if _, err := a.shared().CapabilityProject(root); err == nil {
			var overrides map[string]string
			var err error
			argv, overrides, err = driver.Launch(root, argv, administrative)
			if err != nil {
				return err
			}
			for key, value := range overrides {
				previous, existed := os.LookupEnv(key)
				if err := os.Setenv(key, value); err != nil {
					return err
				}
				defer func(key, previous string, existed bool) {
					if existed {
						_ = os.Setenv(key, previous)
					} else {
						_ = os.Unsetenv(key)
					}
				}(key, previous, existed)
			}
		}
	}
	return shared.Run("", real, argv...)
}
