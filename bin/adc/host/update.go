package host

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// The newly compiled helper owns both installation and runtime lifecycle.
// Prepare everything before stopping agents so build/install failures leave
// the current container running. An intentionally stopped workspace stays so.
func (a *App) ApplyUpdate(install func() error) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	return a.shared().LockedFile("lifecycle.lockfile", func() error { return a.applyUpdate(install) })
}

func (a *App) applyUpdate(install func() error) error {
	if err := a.checkMigration(); err != nil {
		return err
	}
	var c shared.Config
	state := ""
	var prepared *preparedWorkspace
	if _, err := os.Stat(filepath.Join(a.State, "workspace.json")); !os.IsNotExist(err) {
		var err error
		c, err = a.shared().Load()
		if err != nil {
			return err
		}
		if resumed, err := a.resumeReplacement(c, realDocker(), a.replacementWait(c)); resumed || err != nil {
			return err
		}
		existing, err := realDocker().container(a.shared().Container(c))
		if err != nil {
			return fmt.Errorf("не удалось проверить workspace перед обновлением: %w", err)
		}
		if existing != nil {
			state = existing.State.Status
		}
	}
	return updateWorkspace(state, func() error {
		image, err := a.ensureImage()
		if err != nil {
			return err
		}
		// Security profiles, config, image ID and Docker inspection are resolved
		// before installation and before the replacement stops any agents.
		if c.Name != "" {
			var err error
			prepared, err = a.prepareWorkspace(c, image)
			if err != nil {
				return err
			}
			prepared.Replace = true
		}
		return install()
	}, func() error { return a.launchWorkspace(prepared, realDocker(), a.replacementWait(c)) })
}

func updateWorkspace(state string, prepare, restart func() error) error {
	if state == "paused" {
		return fmt.Errorf("workspace приостановлен; выполните docker unpause перед adc update")
	}
	resume := state == "running" || state == "restarting"
	if resume {
		fmt.Println("Workspace работает. После подготовки обновления он будет перезапущен; активные сеансы агентов завершатся.")
	}
	if err := prepare(); err != nil {
		return err
	}
	if !resume {
		fmt.Println("ADC обновлён. Workspace не запущен; запуск: adc workspace start.")
		return nil
	}
	// Replacement owns stop/rename/run and its journal as one recoverable flow.
	if err := restart(); err != nil {
		return fmt.Errorf("ADC обновлён; возобновление workspace: %w; состояние: adc workspace status; повтор: adc workspace start", err)
	}
	return nil
}

// Build and run the new command before installing it, so a compiler failure
// keeps the last working executable. The helper also regenerates completion.
func (a *App) Update() error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	// The parent holds the source lock while the compiled helper takes only the
	// lifecycle lock. Start takes the same locks in this order.
	return a.shared().LockedFile("update.lockfile", a.update)
}

func (a *App) update() error {
	if _, err := os.Stat(a.replacementPath()); err == nil {
		return fmt.Errorf("переключение workspace не завершено: выполните adc workspace start перед следующим adc update")
	} else if !os.IsNotExist(err) {
		return err
	}
	status, err := shared.Output("", "git", "-C", a.Platform, "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("платформа содержит локальные изменения; сохраните их перед adc update; файлы не изменены")
	}
	if err := shared.Run("", "git", "-C", a.Platform, "pull", "--ff-only"); err != nil {
		return err
	}
	name := "go"
	pattern := ".adc-update-*"
	if runtime.GOOS == "windows" {
		name += ".exe"
		pattern += ".exe"
	}
	compiler := filepath.Join(a.State, "toolchains", "go1.26.1", "go", "bin", name)
	if _, err := os.Stat(compiler); err != nil {
		compiler, err = exec.LookPath("go")
		if err != nil {
			return fmt.Errorf("компилятор ADC отсутствует: повторите install.sh/install.ps1: %w", err)
		}
	}
	bin := filepath.Join(a.State, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(bin, pattern)
	if err != nil {
		return err
	}
	helper := file.Name()
	if err = file.Close(); err != nil {
		os.Remove(helper)
		return err
	}
	defer os.Remove(helper)
	env := append(os.Environ(), "ADC_RUNTIME=0", "AI_DEVCONTAINER_HOME="+a.Platform, "ADC_HOME="+a.State, "GOCACHE="+filepath.Join(a.State, "cache/go-build"), "GOMODCACHE="+filepath.Join(a.State, "cache/go-mod"))
	build := exec.Command(compiler, "build", "-trimpath", "-o", helper, "./bin/adc")
	build.Dir, build.Env = a.Platform, env
	build.Stdin, build.Stdout, build.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err = build.Run(); err != nil {
		return fmt.Errorf("новый ADC не собран; прежний бинарник сохранён: %w", err)
	}
	apply := exec.Command(helper, "bootstrap-apply-update")
	apply.Dir, apply.Env = a.Platform, env
	apply.Stdin, apply.Stdout, apply.Stderr = os.Stdin, os.Stdout, os.Stderr
	return apply.Run()
}

func installCommandFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return replaceCommandLink(path, data)
	}
	if runtime.GOOS != "windows" {
		return shared.AtomicWrite(path, data, 0755)
	}
	previous, err := os.ReadFile(path)
	if os.IsNotExist(err) || err == nil && string(previous) == string(data) {
		return shared.AtomicWrite(path, data, 0755)
	}
	if err != nil {
		return err
	}
	// Windows allows renaming a running executable, but cannot overwrite it.
	// Retain the old image until the parent updater exits; no user data is moved.
	file, err := os.CreateTemp(filepath.Dir(path), ".adc-previous-*.exe")
	if err != nil {
		return err
	}
	backup := file.Name()
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Remove(backup); err != nil {
		return err
	}
	if err = os.Rename(path, backup); err != nil {
		return err
	}
	if err = shared.AtomicWrite(path, data, 0755); err != nil {
		if rollback := os.Rename(backup, path); rollback != nil {
			return fmt.Errorf("установка: %w; восстановление: %v; предыдущий бинарник: %s", err, rollback, backup)
		}
		return err
	}
	// Removal can fail while the old updater is running. Its private executable
	// remains available for recovery and can be removed after process exit.
	_ = os.Remove(backup)
	return nil
}

// v0 installs ~/.local/bin/adc as a link to bin/adc. After switching the clone
// that target is a directory of Go source, so reading through the link fails.
// Replace the link itself, keeping its target untouched.
func replaceCommandLink(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".adc-install-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0755); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if runtime.GOOS != "windows" {
		return os.Rename(file.Name(), path)
	}
	link, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		if rollback := os.Symlink(link, path); rollback != nil {
			return fmt.Errorf("замена ссылки: %w; восстановление: %v", err, rollback)
		}
		return err
	}
	return nil
}
