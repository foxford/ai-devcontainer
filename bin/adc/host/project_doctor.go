package host

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// DoctorProject is a read-only checkout inspection, available on both sides.
func (a *App) DoctorProject(target string, out io.Writer) error {
	root, err := shared.Abs(target)
	var registered *shared.Project
	if config, loadErr := a.shared().Load(); loadErr == nil {
		p, projectErr := a.shared().Project(config, target)
		if projectErr != nil && err == nil {
			for _, candidate := range config.Projects {
				candidateRoot, candidateErr := shared.Abs(a.shared().ProjectPath(candidate))
				if candidateErr == nil && candidateRoot == root {
					p, projectErr = candidate, nil
					break
				}
			}
		}
		if projectErr == nil {
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
	containerLinks := 0
	aliases := map[string]string{}
	if !a.Runtime {
		aliases["/workspaces/"+filepath.Base(root)] = root
		aliases["/workspace/projects/"+filepath.Base(root)] = root
		if registered != nil {
			aliases[registered.RuntimePath] = root
			aliases["/workspace/projects/"+registered.ID] = root
		}
	}
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
			container, err := checkProjectLink(file, aliases, !a.Runtime)
			if err != nil {
				failures = append(failures, fmt.Errorf("неисправная ссылка %s → %s: %w", filepath.ToSlash(name), link, err))
			} else if container {
				containerLinks++
			}
			return nil
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	if containerLinks > 0 {
		fmt.Fprintf(out, "INFO: ссылок с контейнерными путями: %d; полная проверка доступна внутри ADC\n", containerLinks)
	}
	for _, err := range failures {
		fmt.Fprintln(out, "ERROR:", err)
	}
	fmt.Fprintln(out, "Проверка завершена; файлы не изменены.")
	if len(failures) > 0 {
		return fmt.Errorf("проверка проекта: ошибок %d; подробности выше", len(failures))
	}
	return nil
}

// Resolve relative chains and symlinked parents without assuming that a
// container path must exist on the host. Known checkout aliases can be checked
// against host files; unresolved runtime stores require a runtime inspection.
func checkProjectLink(file string, aliases map[string]string, host bool) (bool, error) {
	var prefixes []string
	for prefix := range aliases {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	container := false
	for redirects := 0; redirects < 64; redirects++ {
		if host {
			checkout := false
			for _, prefix := range prefixes {
				if file == prefix || strings.HasPrefix(file, prefix+"/") {
					checkout = true
					mapped := filepath.Join(aliases[prefix], filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(file, prefix), "/")))
					if mapped != file {
						file = mapped
						container = true
					}
					break
				}
			}
			if redirects > 0 && !checkout {
				for _, prefix := range []string{"/opt/ai-devcontainer", "/opt/dev-platform", "/opt/ai-tools", "/opt/adc-agents", "/home/node", "/workspaces", "/workspace/projects"} {
					if file == prefix || strings.HasPrefix(file, prefix+"/") {
						return true, nil
					}
				}
			}
		}
		file = filepath.Clean(file)
		anchor := filepath.VolumeName(file) + string(filepath.Separator)
		parts := strings.Split(strings.TrimPrefix(file, anchor), string(filepath.Separator))
		current := anchor
		redirected := false
		for index, part := range parts {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil {
				return container, err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(current)
			if err != nil {
				return container, err
			}
			// Windows stores volume-relative link targets with backslashes.
			// Preserve the Linux namespace before resolving them on the host.
			if slash := filepath.ToSlash(target); strings.HasPrefix(slash, "/") && !strings.HasPrefix(slash, "//") {
				target = slash
			}
			if !shared.PortableAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
				// POSIX runtime namespaces must stay POSIX even on Windows hosts.
				file = path.Join(target, filepath.ToSlash(filepath.Join(parts[index+1:]...)))
			} else {
				file = filepath.Join(append([]string{target}, parts[index+1:]...)...)
			}
			redirected = true
			break
		}
		if !redirected {
			return container, nil
		}
	}
	return container, errors.New("циклическая или слишком длинная цепочка ссылок")
}
