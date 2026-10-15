package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func graphLogPath(root string) string {
	return filepath.Join(root, shared.ProjectStateDir, "graphify.log")
}

func openGraphLog(path string, appendMode bool) (*os.File, error) {
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s должен быть обычным каталогом", directory)
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s должен быть обычным файлом", path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	file, err := openGraphLogFile(path, appendMode)
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s должен быть обычным файлом", path)
	}
	return file, nil
}

func resetGraphLog(path string) error {
	// Windows append-only handles cannot truncate. Open a separate write handle
	// without O_TRUNC so the same path/file checks run before changing its size.
	log, err := openGraphLog(path, false)
	if err != nil {
		return err
	}
	return errors.Join(log.Truncate(0), log.Close())
}

func (a *App) startGraphRefresh(root string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "graphify-refresh", root)
	cmd.Dir = root
	cmd.Env = replaceEnv(os.Environ(), map[string]string{
		"ADC_RUNTIME": "1", "ADC_HOME": a.State, "AI_DEVCONTAINER_HOME": a.Platform,
		"ADC_STARTUP_STATUS": "",
	})
	return startGraphWorker(cmd, graphLogPath(root))
}

// No terminal or command-output pipes may be inherited: their reader would
// otherwise wait for indexing after sync/agent setup has already returned.
func startGraphWorker(cmd *exec.Cmd, logPath string) error {
	log, err := openGraphLog(logPath, true)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = log, log
	detachGraphWorker(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap children when called from the long-lived runtime. Short-lived setup
	// commands can exit immediately; the worker continues under the container init.
	go func() { _ = cmd.Wait() }()
	return nil
}

// RefreshGraph is the automatic worker. Explicit indexing and MCP requests use
// ensureGraph and wait for the same checkout lock before checking freshness.
func (a *App) RefreshGraph(root string) error {
	if err := a.shared().RequireRuntime(); err != nil {
		return err
	}
	p, err := a.shared().CapabilityProject(root)
	if err != nil {
		return err
	}
	if p.ReadOnly || os.Getenv("ADC_INSTALL_AI_TOOLS") == "0" {
		return nil
	}
	caps, err := a.shared().ProjectCapabilities(p)
	if err != nil {
		return err
	}
	if !shared.Contains(caps, "graphify") {
		return nil
	}
	version, err := a.graphVersion()
	if err != nil {
		return err
	}
	_, err = a.graphLocker(p.RuntimePath).TryLocked(func() error {
		// Only the active writer resets the log; a duplicate worker must not
		// truncate diagnostics from the index already being built.
		if err := resetGraphLog(graphLogPath(p.RuntimePath)); err != nil {
			return err
		}
		err = EnsureGraph(p.RuntimePath, false, graphRun, os.Stderr, version)
		if err != nil {
			return fmt.Errorf("Graphify: %w; повтор: adc tools graphify index", err)
		}
		fmt.Fprintln(os.Stderr, "Graphify: индекс актуален", p.RuntimePath)
		return nil
	})
	return err
}
