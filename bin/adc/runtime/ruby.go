package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// initializeRuby seeds only the missing plugin. Installed interpreters, gems,
// shims, plugin updates and version selections belong to the developer.
// The caller holds the shared configuration lock.
func initializeRuby(home, seed string) error {
	plugins := filepath.Join(home, ".asdf", "plugins")
	target := filepath.Join(plugins, "ruby")
	if _, err := os.Lstat(target); err == nil {
		info, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("Ruby-плагин asdf: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("Ruby-плагин asdf: %s должен быть каталогом", target)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(plugins, 0755); err != nil {
		return err
	}
	// Stage beside the target so a failed copy never leaves a partial plugin.
	staging, err := os.MkdirTemp(plugins, ".adc-ruby-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyRuntimeAssets(seed, staging); err != nil {
		return fmt.Errorf("Подготовка Ruby-плагина asdf: %w", err)
	}
	return os.Rename(staging, target)
}
