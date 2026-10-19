package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (claudeDriver) Install(i *toolInstaller) error {
	if i.works("claude") {
		fmt.Fprintln(i.out, "claude уже установлен")
		return nil
	}
	fmt.Fprintln(i.out, "Установка claude через npm")
	// Replace only broken launchers; keep all agent configuration and sessions.
	for _, file := range []string{filepath.Join(i.tools, "bin", "claude"), filepath.Join(i.home, ".local", "bin", "claude")} {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// The official package contains the native binary as an optional dependency.
	// Its postinstall links that binary; no vendor curl download is needed.
	if _, err := i.run(i.home, "npm", "install", "-g", "--include=optional", "--allow-scripts=@anthropic-ai/claude-code", "@anthropic-ai/claude-code"); err != nil {
		return fmt.Errorf("установка Claude через npm: %w", err)
	}
	if !i.works("claude") {
		return errors.New("Claude установлен, но --version не работает")
	}
	return nil
}

func (codexDriver) Install(i *toolInstaller) error {
	if i.works("codex") {
		return nil
	}
	if _, err := i.run(i.home, "npm", "install", "-g", "@openai/codex"); err != nil {
		return err
	}
	if !i.works("codex") {
		return errors.New("Codex не запускается после установки")
	}
	return nil
}

func (openCodeDriver) Install(i *toolInstaller) error {
	if i.works("opencode") {
		fmt.Fprintln(i.out, "opencode уже установлен")
		return nil
	}
	fmt.Fprintln(i.out, "Установка opencode")
	// The release lookup uses quiet curl under set -euo pipefail. Its DNS
	// failures need the vendor retry runner even without diagnostic output.
	if err := i.vendorWithRunner("https://opencode.ai/install", "bash", i.runVendor); err != nil {
		return fmt.Errorf("opencode: %w", err)
	}
	if err := i.promote("opencode", filepath.Join(i.home, ".opencode", "bin", "opencode")); err != nil {
		return err
	}
	if !i.works("opencode") {
		return errors.New("opencode установлен, но --version не работает")
	}
	return nil
}

func (hermesDriver) Install(i *toolInstaller) error {
	if i.works("hermes") {
		fmt.Fprintln(i.out, "hermes уже установлен")
		return nil
	}
	fmt.Fprintln(i.out, "Установка hermes")
	if err := os.RemoveAll(filepath.Join(i.home, ".hermes", "hermes-agent")); err != nil {
		return err
	}
	if err := i.vendor("https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh", "bash"); err != nil {
		return fmt.Errorf("hermes: %w", err)
	}
	if err := i.promote("hermes", filepath.Join(i.home, ".local", "bin", "hermes")); err != nil {
		return err
	}
	if !i.works("hermes") {
		return errors.New("hermes установлен, но --version не работает")
	}
	return nil
}

func (dshDriver) Install(i *toolInstaller) error {
	if i.works("dsh") {
		return nil
	}
	_, err := i.run(i.home, "npm", "install", "-g", "--allow-scripts=@deepseek-ai/dsh-subprocess-local,node-pty,koffi", "@deepseek-ai/dsh")
	if err != nil {
		_, err = i.run(i.home, "npm", "install", "-g", "@deepseek-ai/dsh")
	}
	if err != nil {
		fmt.Fprintln(i.out, "DSH не установлен:", err)
	}
	return nil
}
