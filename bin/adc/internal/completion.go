package shared

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func CompletionFilename(shell string) string {
	if shell == "powershell" {
		return "adc.ps1"
	}
	return "adc." + shell
}

func CompletionScript(root *cobra.Command, shell string, descriptions bool) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(&buf, descriptions)
	case "zsh":
		if descriptions {
			err = root.GenZshCompletion(&buf)
		} else {
			err = root.GenZshCompletionNoDesc(&buf)
		}
	case "fish":
		err = root.GenFishCompletion(&buf, descriptions)
	case "powershell":
		if descriptions {
			err = root.GenPowerShellCompletionWithDesc(&buf)
		} else {
			err = root.GenPowerShellCompletion(&buf)
		}
	default:
		return nil, fmt.Errorf("неподдерживаемая оболочка: %s", shell)
	}
	return buf.Bytes(), err
}

// Runtime help differs from host help. Its scripts must never replace the
// host's completions in the shared ADC_HOME mount.
func (a *App) CompletionPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if shell == "fish" {
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		return filepath.Abs(filepath.Join(config, "fish", "completions", CompletionFilename(shell)))
	}
	base := filepath.Join(a.State, "completions")
	if a.Runtime {
		base = filepath.Join(home, ".local", "share", "adc", "completions")
	}
	return filepath.Abs(filepath.Join(base, CompletionFilename(shell)))
}

// Instructions are data for the user's shell; ADC never executes them or
// edits a profile when the user only asks to save completion.
func CompletionInstructions(shell, path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var session, persistent string
	switch shell {
	case "bash", "zsh":
		profile := filepath.Join(home, ".bashrc")
		lines := []string{"source " + ShellQuote(path)}
		if shell == "zsh" {
			dir := os.Getenv("ZDOTDIR")
			if dir == "" {
				dir = home
			}
			profile = filepath.Join(dir, ".zshrc")
			lines = append([]string{"if ! command -v compdef >/dev/null 2>&1; then autoload -Uz compinit; compinit; fi"}, lines...)
		}
		session = strings.Join(lines, "; ")
		quoted := make([]string, len(lines))
		for i, line := range lines {
			quoted[i] = ShellQuote(line)
		}
		persistent = "Добавить в " + profile + ":\n  printf '%s\\n' " + strings.Join(quoted, " ") + " >> " + ShellQuote(profile)
	case "fish":
		session = "source " + ShellQuote(path)
		persistent = "Fish подключит этот файл автоматически в новых сессиях."
	case "powershell":
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
		session = ". " + quote(path)
		persistent = "Добавить в $PROFILE.CurrentUserAllHosts:\n  New-Item -ItemType Directory -Force (Split-Path $PROFILE.CurrentUserAllHosts) | Out-Null; Add-Content -LiteralPath $PROFILE.CurrentUserAllHosts -Value " + quote(session)
	default:
		return "", fmt.Errorf("неподдерживаемая оболочка: %s", shell)
	}
	return "Подключить в текущей сессии:\n  " + session + "\n" + persistent + "\n", nil
}
