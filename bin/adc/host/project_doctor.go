package host

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// DoctorProject is a read-only checkout inspection, available on both sides.
func (a *App) DoctorProject(target string, out io.Writer) error {
	root, err := shared.Abs(target)
	var registered *shared.Project
	if config, loadErr := a.shared().Load(); loadErr == nil {
		if p, projectErr := a.shared().Project(config, target); projectErr == nil {
			registered = &p
			path := p.Path
			if a.Runtime {
				path = p.RuntimePath
			}
			root, err = shared.Abs(path)
		}
	}
	if err != nil {
		return err
	}
	gitRoot, err := shared.Output(root, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("укажите корень существующего Git-репозитория")
	}
	gitRoot, err = shared.Abs(gitRoot)
	if err != nil || root != gitRoot {
		return errors.New("укажите корень Git-репозитория, а не вложенный каталог")
	}
	fmt.Fprintln(out, "Проект:", root)
	if registered == nil {
		fmt.Fprintln(out, "WARN: проект ещё не подключён к ADC")
	} else {
		fmt.Fprintln(out, "OK: подключён как", registered.ID)
		if registered.ReadOnly {
			fmt.Fprintln(out, "INFO: read-only, доставка не изменяет проект")
		}
	}
	var failures []error
	for _, name := range []string{".devcontainer", shared.LegacyProjectStateDir, shared.ProjectStateDir} {
		info, err := os.Lstat(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if name == ".devcontainer" {
			fmt.Fprintln(out, "WARN: .devcontainer остался от проектной среды; новый ADC его не использует")
			continue
		}
		if !info.IsDir() {
			failures = append(failures, fmt.Errorf("%s должен быть обычным каталогом", name))
			continue
		}
		if name == shared.LegacyProjectStateDir {
			fmt.Fprintln(out, "WARN: .ai-devcontainer — старое состояние; adc sync скопирует его в .adc")
		} else {
			fmt.Fprintln(out, "OK: служебное состояние .adc")
		}
	}
	if info, err := os.Lstat(filepath.Join(root, "AGENTS.platform.md")); os.IsNotExist(err) {
		fmt.Fprintln(out, "WARN: AGENTS.platform.md отсутствует; требуется adc sync")
	} else if err != nil {
		failures = append(failures, err)
	} else if info.Mode().IsRegular() && strings.Contains(cleanupHeader(root, "AGENTS.platform.md"), "ai-devcontainer:generated") {
		fmt.Fprintln(out, "OK: AGENTS.platform.md доставлен настоящим файлом")
	} else {
		fmt.Fprintln(out, "WARN: AGENTS.platform.md — ссылка или проектный файл; проверьте перед новой доставкой")
	}
	checked := shared.Project{RuntimePath: root, Path: root}
	if registered != nil {
		checked.Capabilities = registered.Capabilities
	}
	if _, err := a.shared().ProjectCapabilities(checked); err != nil {
		failures = append(failures, fmt.Errorf("возможности проекта: %w", err))
	}
	paths := []string{".agents", ".claude/skills", ".dsh/skills", ".hermes/skills", "plans", "AGENTS.platform.md", "AGENTS.skills.md", "MONOREPO.md", ".mcp.json"}
	for _, rel := range paths {
		if _, err := cleanupInfo(root, rel); os.IsNotExist(err) {
			continue
		} else if err != nil {
			fmt.Fprintln(out, "WARN:", rel, "— родитель недоступен или является ссылкой")
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(rel)), func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink == 0 {
				return nil
			}
			name, _ := filepath.Rel(root, file)
			link, err := os.Readlink(file)
			if err != nil {
				return err
			}
			if !a.Runtime && (strings.HasPrefix(link, "/opt/") || strings.HasPrefix(link, "/home/node/")) {
				fmt.Fprintf(out, "INFO: %s → %s — путь внутри контейнера\n", filepath.ToSlash(name), link)
				return nil
			}
			if _, err := os.Stat(file); err != nil {
				failures = append(failures, fmt.Errorf("неисправная ссылка %s → %s", filepath.ToSlash(name), link))
			}
			return nil
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	for _, err := range failures {
		fmt.Fprintln(out, "ERROR:", err)
	}
	fmt.Fprintln(out, "Проверка завершена; файлы не изменены.")
	return errors.Join(failures...)
}
