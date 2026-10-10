package runtime

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// Migration populates the home volume before Docker can seed it from the image.
// Keep defaults outside that volume and restore only missing shell assets.
func initializeShells(home, seed string) error {
	framework := filepath.Join(home, ".oh-my-zsh")
	if _, err := os.Lstat(framework); os.IsNotExist(err) {
		staging, err := os.MkdirTemp(home, ".adc-zsh-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		if err := copyShellAssets(filepath.Join(seed, ".oh-my-zsh"), staging); err != nil {
			return err
		}
		if err := os.Rename(staging, framework); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for _, file := range []string{".zshrc", ".bashrc"} {
		profile := filepath.Join(home, file)
		old, err := os.ReadFile(profile)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		// The previous initializer created profiles containing only this block.
		user := string(old)
		start, end := strings.Index(user, "# adc:agents:start"), strings.Index(user, "# adc:agents:end")
		if start >= 0 && end >= start {
			user = user[:start] + user[end+len("# adc:agents:end"):]
		}
		if strings.TrimSpace(user) == "" {
			data, err := os.ReadFile(filepath.Join(seed, file))
			if err != nil {
				return err
			}
			if err := shared.AtomicWrite(profile, data, 0644); err != nil {
				return err
			}
		}
		body := ". /opt/ai-devcontainer/runtime/shell-env.sh\n"
		if file == ".zshrc" {
			body += `setopt INC_APPEND_HISTORY SHARE_HISTORY HIST_IGNORE_DUPS HIST_IGNORE_SPACE
case "$PS1" in *"[${ADC_WORKSPACE}]"*) ;; *)
  PS1="%F{cyan}[${ADC_WORKSPACE}]%f $PS1" ;;
esac
`
		} else {
			body += "shopt -s histappend\n"
			body += `case "${PROMPT_COMMAND:-}" in *"history -a"*) ;; *) PROMPT_COMMAND="history -a;${PROMPT_COMMAND:-}" ;; esac` + "\n"
		}
		body += `if [ -t 1 ] && [ "${ADC_BANNER_SHOWN:-}" != 1 ]; then adc banner; ADC_BANNER_SHOWN=1; fi`
		if err := shared.ManagedBlock(profile, "# adc:agents:start", "# adc:agents:end", body); err != nil {
			return err
		}
	}
	return nil
}

func copyShellAssets(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(dest, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, dest)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("неподдерживаемый файл оболочки: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, info.Mode().Perm())
	})
}
