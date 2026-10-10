package runtime

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"go.yaml.in/yaml/v3"
)

const skillMarker = ".ai-devcontainer-wired"

var dshSkillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type skillLayers struct{ stock, project string }

func skillFiles(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = path
		return nil
	})
	return files, err
}

func (a *App) skills(root string, selected ...[]string) (map[string]skillLayers, error) {
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return nil, err
	}
	layers := map[string]skillLayers{}
	for index, dir := range []string{a.shared().DeliveryPath(catalog.Base.Skills), filepath.Join(root, ".agents", "skills")} {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "README.md" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			info, err := os.Stat(path)
			if err != nil {
				if index == 1 && os.IsNotExist(err) && entry.Type()&os.ModeSymlink != 0 {
					fmt.Fprintln(os.Stderr, "Скилл пропущен: отсутствует цель ссылки", path)
					continue
				}
				return nil, err
			}
			if !info.IsDir() {
				continue
			}
			layer := layers[entry.Name()]
			if index == 0 {
				if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
					continue
				}
				layer.stock = path
			} else {
				layer.project = path
			}
			layers[entry.Name()] = layer
		}
	}
	project := shared.Project{RuntimePath: root}
	if len(selected) == 0 && a.State != "" {
		config, err := a.shared().Load()
		if err != nil {
			return nil, err
		}
		project, err = a.shared().Project(config, root)
		if err != nil {
			project, err = a.FindCheckout(config, root)
		}
		if err != nil {
			return nil, err
		}
		project.RuntimePath = root
	}
	capabilities, err := a.shared().ProjectCapabilities(project)
	if len(selected) > 0 {
		capabilities = selected[0]
		err = nil
	}
	if err != nil {
		return nil, err
	}
	for _, capability := range capabilities {
		for _, source := range catalog.Capabilities[capability].Skills {
			name := filepath.Base(source)
			layer := layers[name]
			if layer.stock != "" && layer.stock != a.shared().DeliveryPath(source) {
				return nil, fmt.Errorf("конфликт источников скилла %s", name)
			}
			layer.stock = a.shared().DeliveryPath(source)
			layers[name] = layer
		}
	}
	return layers, nil
}

func skillNames(layers map[string]skillLayers) []string {
	names := make([]string, 0, len(layers))
	for name := range layers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isCode(path string) bool {
	switch filepath.Ext(path) {
	case ".mjs", ".js", ".cjs", ".ts", ".tsx", ".py", ".sh":
		return true
	}
	return false
}

func copySkillFile(source, dest string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err := shared.AtomicWrite(dest, data, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(dest, info.ModTime(), info.ModTime())
}

func (a *App) DeliverSkills(root string, selected ...[]string) error {
	layers, err := a.skills(root, selected...)
	if err != nil {
		return err
	}
	dest := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := layers[entry.Name()]; ok {
			continue
		}
		path := filepath.Join(dest, entry.Name())
		if _, err := os.Stat(filepath.Join(path, skillMarker)); err == nil {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	live := map[string]bool{}
	var index strings.Builder
	index.WriteString(shared.Marker + "# Скиллы проекта\n\nПлатформенные скиллы дополняются файлами из `.agents/skills/`. Проектный файл перекрывает платформенный. Читай собранный SKILL.md из `.claude/skills/`; код скопирован, чтобы относительные импорты учитывали форки.\n\n| Скилл | Источник | Описание |\n|---|---|---|\n")
	for _, name := range skillNames(layers) {
		target := filepath.Join(dest, name)
		if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink == 0 {
			if _, err := os.Stat(filepath.Join(target, skillMarker)); err != nil {
				fmt.Fprintln(os.Stderr, "Скилл сохранён без изменений:", target, "; для управляемого форка используйте .agents/skills")
				fmt.Fprintf(&index, "| [%s](.claude/skills/%s/SKILL.md) | пользовательский каталог | Сохранён без изменений; платформенная версия не доставлена |\n", name, name)
				live[name] = true
				continue
			}
		}
		layer := layers[name]
		stock, err := skillFiles(layer.stock)
		if err != nil {
			return err
		}
		project, err := skillFiles(layer.project)
		if err != nil {
			return err
		}
		merged := map[string]string{}
		for rel, path := range stock {
			merged[rel] = path
		}
		for rel, path := range project {
			merged[rel] = path
		}
		if _, ok := merged["SKILL.md"]; !ok {
			fmt.Fprintln(os.Stderr, "скилл без SKILL.md:", name)
			continue
		}
		staging, err := os.MkdirTemp(dest, ".adc-skill-")
		if err != nil {
			return err
		}
		build := func() error {
			defer os.RemoveAll(staging)
			for rel, source := range merged {
				path := filepath.Join(staging, rel)
				if isCode(rel) {
					err = copySkillFile(source, path)
				} else {
					err = replaceLink(path, source)
				}
				if err != nil {
					return err
				}
			}
			if err := shared.AtomicWrite(filepath.Join(staging, skillMarker), nil, 0644); err != nil {
				return err
			}
			if err := os.RemoveAll(target); err != nil {
				return err
			}
			return os.Rename(staging, target)
		}
		if err := build(); err != nil {
			return err
		}
		live[name] = true
		origin := "платформа"
		if layer.stock == "" {
			origin = "проект"
		} else if len(project) > 0 {
			origin = fmt.Sprintf("платформа + форк: %d файлов", len(project))
		}
		data, err := os.ReadFile(merged["SKILL.md"])
		if err != nil {
			return err
		}
		description := frontmatterField(string(data), "description")
		description = strings.ReplaceAll(strings.ReplaceAll(description, "|", "\\|"), "\n", " ")
		fmt.Fprintf(&index, "| [%s](.claude/skills/%s/SKILL.md) | %s | %s |\n", name, name, origin, description)
	}
	if err := shared.AtomicWrite(filepath.Join(root, "AGENTS.skills.md"), []byte(index.String()), 0644); err != nil {
		return err
	}
	dsh := filepath.Join(root, ".dsh", "skills")
	if err := os.MkdirAll(dsh, 0755); err != nil {
		return err
	}
	entries, err = os.ReadDir(dsh)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dsh, entry.Name())
		link, err := os.Readlink(path)
		if err == nil && managedDSHLink(root, entry.Name(), link) && !live[entry.Name()] {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	for name := range live {
		if !dshSkillName.MatchString(name) {
			fmt.Fprintln(os.Stderr, "DSH: имя не kebab-case:", name)
			continue
		}
		path := filepath.Join(dsh, name)
		if _, err := os.Lstat(path); err == nil {
			link, err := os.Readlink(path)
			if err != nil || !managedDSHLink(root, name, link) {
				continue
			}
		}
		if err := replaceLink(path, filepath.Join("..", "..", ".claude", "skills", name)); err != nil {
			return err
		}
	}
	return shared.ManagedBlock(filepath.Join(root, "AGENTS.md"), "<!-- adc:skills:start -->", "<!-- adc:skills:end -->", "Скиллы и их область применения: [AGENTS.skills.md](AGENTS.skills.md).")
}

func managedDSHLink(root, name, link string) bool {
	if !filepath.IsAbs(link) {
		link = filepath.Join(root, ".dsh", "skills", link)
	}
	for _, base := range []string{root, "/workspaces/" + filepath.Base(root)} {
		if filepath.Clean(link) == filepath.Join(base, ".claude", "skills", name) {
			return true
		}
	}
	return false
}

func frontmatterField(text, key string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	end := -1
	for index, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			end = index + 1
			break
		}
	}
	if end < 0 {
		return ""
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &document); err != nil {
		return ""
	}
	if len(document.Content) > 0 && document.Content[0].Kind == yaml.MappingNode {
		if value := yamlField(document.Content[0], key); value != nil && value.Kind == yaml.ScalarNode {
			return value.Value
		}
	}
	return ""
}
