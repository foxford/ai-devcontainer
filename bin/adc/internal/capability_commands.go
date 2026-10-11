package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CapabilityProject resolves the current checkout, including linked worktrees,
// using paths for the execution side. RuntimePath then points at this checkout
// so team settings are read from it rather than from the primary repository.
func (a *App) CapabilityProject(dir string) (Project, error) {
	c, err := a.Load()
	if err != nil {
		return Project{}, err
	}
	root, err := Output(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return Project{}, errors.New("запустите команду из подключённого Git-проекта")
	}
	root, err = Abs(root)
	if err != nil {
		return Project{}, err
	}
	common, err := Output(root, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Project{}, err
	}
	common, err = Abs(common)
	if err != nil {
		return Project{}, err
	}
	for _, p := range c.Projects {
		candidate, pathErr := Abs(a.ProjectPath(p))
		if pathErr == nil && candidate == root {
			p.RuntimePath = root
			return p, nil
		}
	}
	for _, p := range c.Projects {
		other, gitErr := Output(a.ProjectPath(p), "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
		if gitErr != nil {
			continue
		}
		other, gitErr = Abs(other)
		if gitErr == nil && other == common {
			p.RuntimePath = root
			return p, nil
		}
	}
	return Project{}, errors.New("текущий проект не подключён: adc project add")
}

// WriteCapabilities displays the full catalog, even without an initialized
// workspace. Checkmarks describe effective selection, not tool health.
func (a *App) WriteCapabilities(dir string, out io.Writer, color bool) error {
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		return err
	}
	selected := []string{}
	p, projectErr := a.CapabilityProject(dir)
	if projectErr == nil {
		selected, err = a.ProjectCapabilities(p)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "Проект:", p.ID, "→", p.RuntimePath)
	} else {
		fmt.Fprintln(out, "Каталог возможностей; текущий проект не выбран.")
	}
	names := make([]string, 0, len(catalog.Capabilities))
	for name := range catalog.Capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([][]string, 0, len(names))
	for _, name := range names {
		check := "—"
		if Contains(selected, name) {
			check = "✓"
		}
		capability := catalog.Capabilities[name]
		mcp := strings.Join(capability.MCP, ", ")
		if mcp == "" {
			mcp = "—"
		}
		rows = append(rows, []string{check, name, capability.Description, mcp})
	}
	var table bytes.Buffer
	if err = WriteTable(&table, []string{"Выбрано", "Возможность", "Описание", "MCP"}, rows); err != nil {
		return err
	}
	output := table.String()
	if color {
		output = strings.ReplaceAll(output, "✓", "\x1b[32m✓\x1b[0m")
	}
	if _, err = io.WriteString(out, output); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "\n✓ выбрано для текущего проекта; доступность инструментов здесь не проверяется.")
	return err
}

// ChangeCapabilities preserves unrelated JSON fields and validates the entire
// proposed composition before writing any project settings.
func (a *App) ChangeCapabilities(dir, action string, names []string) (Project, error) {
	var project Project
	err := a.Locked(func() error {
		var err error
		project, err = a.CapabilityProject(dir)
		if err != nil {
			return err
		}
		if project.ReadOnly {
			return errors.New("проект подключён read-only; возможности не изменены")
		}
		if action != "add" && action != "rm" || len(names) == 0 {
			return errors.New("adc project capability add|rm <имя> [имя...]")
		}
		catalog, err := a.DeliveryCatalog()
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, ok := catalog.Capabilities[name]; !ok {
				return fmt.Errorf("неизвестная возможность %s; adc project capability list", name)
			}
			if action == "rm" && Contains(project.Capabilities, name) {
				return fmt.Errorf("%s выбрана в регистрации workspace; удалите её из capabilities в workspace.json при остановленной среде", name)
			}
		}
		root := project.RuntimePath
		// A shared team file must not redirect writes outside this checkout.
		for _, path := range []string{filepath.Join(root, ".agents"), filepath.Join(root, ".agents", "project.json")} {
			info, err := os.Lstat(path)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%s — ссылка; используйте обычный проектный файл и каталог", path)
			}
		}
		path := filepath.Join(root, ".agents", "project.json")
		settings := map[string]json.RawMessage{}
		mode := os.FileMode(0644)
		data, err := os.ReadFile(path)
		if err == nil {
			if err = json.Unmarshal(data, &settings); err != nil || settings == nil {
				return fmt.Errorf(".agents/project.json должен содержать JSON объект: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			mode = info.Mode().Perm()
		} else if !os.IsNotExist(err) {
			return err
		}
		team := []string{}
		if value, ok := settings["capabilities"]; ok {
			if err = json.Unmarshal(value, &team); err != nil {
				return fmt.Errorf(".agents/project.json capabilities: %w", err)
			}
		}
		for _, name := range names {
			if action == "add" {
				if !Contains(team, name) && !Contains(project.Capabilities, name) {
					team = append(team, name)
				}
			} else {
				kept := []string{}
				for _, current := range team {
					if current != name {
						kept = append(kept, current)
					}
				}
				team = kept
			}
		}
		if _, err = validateCapabilities(catalog, project.Capabilities, team); err != nil {
			return err
		}
		settings["capabilities"], err = json.Marshal(team)
		if err != nil {
			return err
		}
		data, err = json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return err
		}
		return AtomicWrite(path, append(data, '\n'), mode)
	})
	return project, err
}
