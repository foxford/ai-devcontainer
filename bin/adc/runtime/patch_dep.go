package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"go.yaml.in/yaml/v3"
)

type commandOutput func(dir, name string, args ...string) (string, error)

var packageName = regexp.MustCompile(`^(?:@[a-zA-Z0-9_.-]+/)?[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)
var pnpmStorePackage = regexp.MustCompile(`^(.+?)@([0-9][^_(]*)`)

type depHolder struct {
	Name                      string
	Ranges, Fields, Declarers []string
	Unknown                   bool
}

func packageDocument(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	err = json.Unmarshal(data, &doc)
	if doc == nil && err == nil {
		err = errors.New("package.json должен быть объектом")
	}
	return doc, err
}

func dependencyHolders(root, pkg, version string) ([]depHolder, error) {
	target, ok := parseSemVersion(version)
	if !ok || target.parts != 3 {
		return nil, errors.New("неверная версия")
	}
	entries, err := os.ReadDir(filepath.Join(root, "node_modules", ".pnpm"))
	if err != nil {
		return nil, err
	}
	holders := map[string]depHolder{}
	for _, entry := range entries {
		m := pnpmStorePackage.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		name := strings.ReplaceAll(m[1], "+", "/")
		if name == pkg {
			continue
		}
		doc, err := packageDocument(filepath.Join(root, "node_modules", ".pnpm", entry.Name(), "node_modules", filepath.FromSlash(name), "package.json"))
		if err != nil {
			continue
		}
		for _, field := range []string{"dependencies", "peerDependencies", "optionalDependencies"} {
			deps, _ := doc[field].(map[string]any)
			rangeText, _ := deps[pkg].(string)
			if rangeText == "" {
				continue
			}
			allowed, known := rangeAllows(version, rangeText)
			if allowed || known && !touchesMajor(rangeText, target.numbers[0]) {
				continue
			}
			holder := holders[name]
			holder.Name = name
			holder.Unknown = holder.Unknown || !known
			if !shared.Contains(holder.Ranges, rangeText) {
				holder.Ranges = append(holder.Ranges, rangeText)
			}
			if !shared.Contains(holder.Fields, field) {
				holder.Fields = append(holder.Fields, field)
			}
			holders[name] = holder
		}
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || shared.Contains([]string{"node_modules", "dist", "build", "coverage", "out", "tmp"}, entry.Name()) || len(strings.Split(filepath.ToSlash(rel), "/")) > 4) {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() != "package.json" {
			return nil
		}
		doc, err := packageDocument(path)
		if err != nil {
			return nil
		}
		declarer, _ := doc["name"].(string)
		if declarer == "" {
			declarer = filepath.ToSlash(filepath.Dir(rel))
		}
		for name, holder := range holders {
			for _, field := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
				deps, _ := doc[field].(map[string]any)
				if _, ok := deps[name]; ok && !shared.Contains(holder.Declarers, declarer) {
					holder.Declarers = append(holder.Declarers, declarer)
				}
			}
			holders[name] = holder
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := []depHolder{}
	for _, name := range sortedKeys(holders) {
		result = append(result, holders[name])
	}
	return result, nil
}

type fileBackup struct {
	path   string
	data   []byte
	mode   fs.FileMode
	exists bool
}

func captureFile(path string) (fileBackup, error) {
	b := fileBackup{path: path, mode: 0644}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	if !info.Mode().IsRegular() {
		return b, fmt.Errorf("%s должен быть обычным файлом", path)
	}
	b.exists, b.mode = true, info.Mode().Perm()
	b.data, err = os.ReadFile(path)
	return b, err
}
func (b fileBackup) restore() error {
	if b.exists {
		return shared.AtomicWrite(b.path, b.data, b.mode)
	}
	err := os.Remove(b.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func overrideFile(root string, run commandOutput) (string, int, error) {
	version, err := run(root, "pnpm", "--version")
	if err != nil {
		return "", 0, err
	}
	major, err := strconv.Atoi(strings.Split(strings.TrimSpace(version), ".")[0])
	if err != nil {
		return "", 0, err
	}
	file := "package.json"
	if major >= 11 {
		file = "pnpm-workspace.yaml"
	}
	return filepath.Join(root, file), major, nil
}
func editOverride(path, key, version string) error {
	if strings.HasSuffix(path, ".json") {
		doc, err := packageDocument(path)
		if err != nil {
			return err
		}
		pnpm, ok := doc["pnpm"].(map[string]any)
		if !ok {
			if doc["pnpm"] != nil {
				return errors.New("pnpm должен быть объектом")
			}
			pnpm = map[string]any{}
			doc["pnpm"] = pnpm
		}
		overrides, ok := pnpm["overrides"].(map[string]any)
		if !ok {
			if pnpm["overrides"] != nil {
				return errors.New("overrides должен быть объектом")
			}
			overrides = map[string]any{}
			pnpm["overrides"] = overrides
		}
		overrides[key] = version
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		return shared.AtomicWrite(path, append(data, '\n'), 0644)
	}
	doc, err := yamlMapping(path)
	if err != nil {
		return err
	}
	root := doc.Content[0]
	overrides := yamlField(root, "overrides")
	if overrides == nil || overrides.Tag == "!!null" {
		overrides = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSet(root, "overrides", overrides)
	}
	if overrides.Kind != yaml.MappingNode {
		return errors.New("overrides должен быть YAML-объектом")
	}
	overrides.Style = 0
	yamlSet(overrides, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: version})
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return shared.AtomicWrite(path, data, 0644)
}
func lockedVersions(path, pkg, version string) (outdated, current int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0, 0, err
	}
	target, _ := parseSemVersion(version)
	pattern := regexp.MustCompile(`^/?` + regexp.QuoteMeta(pkg) + `@([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)`)
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for index := 0; index+1 < len(node.Content); index += 2 {
				key := node.Content[index]
				if m := pattern.FindStringSubmatch(key.Value); m != nil {
					v, ok := parseSemVersion(m[1])
					if ok && v.numbers[0] == target.numbers[0] {
						if compareVersion(v, target) < 0 {
							outdated++
						} else {
							current++
						}
					}
				}
				walk(node.Content[index+1])
			}
		} else {
			for _, child := range node.Content {
				walk(child)
			}
		}
	}
	walk(&doc)
	return outdated, current, nil
}
func resolveVersion(root, pkg, version string, run commandOutput) (string, error) {
	if exactVersion.MatchString(version) {
		return version, nil
	}
	query := pkg
	if version != "" {
		if _, err := strconv.ParseUint(version, 10, 32); err != nil {
			return "", errors.New("требуется точная semver-версия или major")
		}
		query += "@" + version
	}
	text, err := run(root, "npm", "view", query, "version", "--json")
	if err != nil {
		return "", err
	}
	var scalar string
	if err := json.Unmarshal([]byte(text), &scalar); err == nil && exactVersion.MatchString(scalar) {
		return scalar, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(text), &list); err != nil || len(list) == 0 {
		return "", errors.New("npm не вернул версию")
	}
	scalar = list[0]
	for _, candidate := range list {
		a, aok := parseSemVersion(candidate)
		b, bok := parseSemVersion(scalar)
		if !aok || !bok {
			return "", errors.New("npm вернул некорректную версию")
		}
		if compareVersion(a, b) > 0 {
			scalar = candidate
		}
	}
	if !exactVersion.MatchString(scalar) {
		return "", errors.New("npm вернул некорректную версию")
	}
	return scalar, nil
}

func PatchDependency(root, pkg, version string, run commandOutput, out io.Writer) (result error) {
	if !packageName.MatchString(pkg) {
		return errors.New("некорректное имя npm-пакета")
	}
	version, err := resolveVersion(root, pkg, version, run)
	if err != nil {
		return err
	}
	if _, err := packageDocument(filepath.Join(root, "package.json")); err != nil {
		return err
	}
	lock := filepath.Join(root, "pnpm-lock.yaml")
	old, _, err := lockedVersions(lock, pkg, version)
	if err != nil {
		return err
	}
	if old == 0 {
		fmt.Fprintln(out, "Нет устаревших версий", pkg, "в выбранном major")
		return nil
	}
	path, _, err := overrideFile(root, run)
	if err != nil {
		return err
	}
	backups := []fileBackup{}
	var override fileBackup
	for _, file := range []string{"package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml"} {
		b, err := captureFile(filepath.Join(root, file))
		if err != nil {
			return err
		}
		backups = append(backups, b)
		if b.path == path {
			override = b
		}
	}
	defer func() {
		if result != nil {
			for _, b := range backups {
				result = errors.Join(result, b.restore())
			}
			result = fmt.Errorf("обновление не завершено; manifest и lock восстановлены: %w", result)
		}
	}()
	target, _ := parseSemVersion(version)
	key := fmt.Sprintf("%s@>=%d.0.0 <%s", pkg, target.numbers[0], version)
	if err := editOverride(path, key, version); err != nil {
		return err
	}
	if text, err := run(root, "pnpm", "install", "--force"); err != nil {
		return fmt.Errorf("pnpm install --force: %w: %s", err, text)
	}
	if err := override.restore(); err != nil {
		return err
	}
	if text, err := run(root, "pnpm", "install"); err != nil {
		return fmt.Errorf("pnpm install: %w: %s", err, text)
	}
	remaining, patched, err := lockedVersions(lock, pkg, version)
	if err != nil {
		return err
	}
	permanent := remaining > 0 || patched == 0
	if permanent {
		if err := editOverride(path, key, version); err != nil {
			return err
		}
		if text, err := run(root, "pnpm", "install"); err != nil {
			return fmt.Errorf("pnpm install с постоянным override: %w: %s", err, text)
		}
		remaining, patched, err = lockedVersions(lock, pkg, version)
		if err != nil {
			return err
		}
		if remaining > 0 || patched == 0 {
			return errors.New("pnpm не закрепил целевую версию")
		}
	}
	fmt.Fprintln(out, pkg, version, "закреплён в lock-файле")
	if permanent {
		fmt.Fprintln(out, "Постоянный override:", key, "в", path)
		holders, err := dependencyHolders(root, pkg, version)
		if err == nil {
			printHolders(out, holders)
		}
	}
	return nil
}
func printHolders(out io.Writer, holders []depHolder) {
	for _, holder := range holders {
		fmt.Fprintf(out, "  %s: %s (%s)\n", holder.Name, strings.Join(holder.Ranges, ", "), strings.Join(holder.Fields, ", "))
		if len(holder.Declarers) > 0 {
			fmt.Fprintln(out, "    объявлен в:", strings.Join(holder.Declarers, ", "))
		} else {
			fmt.Fprintln(out, "    цепочка:", "pnpm why", holder.Name)
		}
		if holder.Unknown {
			fmt.Fprintln(out, "    диапазон требует ручной проверки")
		}
	}
}

type declaredOverride struct{ File, Key, Version string }

func declaredOverrides(root string) ([]declaredOverride, error) {
	result := []declaredOverride{}
	for _, file := range []string{"package.json", "pnpm-workspace.yaml"} {
		path := filepath.Join(root, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		values := map[string]string{}
		if strings.HasSuffix(file, ".json") {
			doc, err := packageDocument(path)
			if err != nil {
				return nil, err
			}
			pnpm, _ := doc["pnpm"].(map[string]any)
			overrides, _ := pnpm["overrides"].(map[string]any)
			for key, value := range overrides {
				if value, ok := value.(string); ok {
					values[key] = value
				}
			}
		} else {
			doc, err := yamlMapping(path)
			if err != nil {
				return nil, err
			}
			mapping := yamlField(doc.Content[0], "overrides")
			if mapping != nil && mapping.Kind == yaml.MappingNode {
				for index := 0; index+1 < len(mapping.Content); index += 2 {
					if mapping.Content[index+1].Tag == "!!str" {
						values[mapping.Content[index].Value] = mapping.Content[index+1].Value
					}
				}
			}
		}
		for _, key := range sortedKeys(values) {
			result = append(result, declaredOverride{file, key, values[key]})
		}
	}
	return result, nil
}
func overridePackage(key string) string {
	if index := strings.LastIndex(key, "@"); index > 0 {
		return key[:index]
	}
	return key
}
func PatchDoctor(root string, run commandOutput, out io.Writer) error {
	if _, err := os.Stat(filepath.Join(root, "node_modules", ".pnpm")); err != nil {
		return errors.New("сначала выполните pnpm install: проверяется установленное дерево")
	}
	_, major, err := overrideFile(root, run)
	if err != nil {
		return err
	}
	overrides, err := declaredOverrides(root)
	if err != nil {
		return err
	}
	actions := 0
	for _, override := range overrides {
		fmt.Fprintf(out, "%s → %s (%s)\n", override.Key, override.Version, override.File)
		if major >= 11 && override.File == "package.json" {
			actions++
			fmt.Fprintln(out, "  pnpm >=11 игнорирует этот override; перенесите его в pnpm-workspace.yaml")
		}
		pkg := overridePackage(override.Key)
		if !packageName.MatchString(pkg) || !exactVersion.MatchString(override.Version) {
			fmt.Fprintln(out, "  сложный override: проверьте вручную")
			continue
		}
		holders, err := dependencyHolders(root, pkg, override.Version)
		if err != nil {
			return err
		}
		if len(holders) == 0 {
			actions++
			fmt.Fprintln(out, "  Установленные зависимости больше не удерживают старую версию; override можно снять")
			continue
		}
		printHolders(out, holders)
		allLift := true
		for _, holder := range holders {
			text, err := run(root, "npm", "view", holder.Name+"@latest", "--json")
			if err != nil {
				allLift = false
				fmt.Fprintln(out, "  реестр недоступен или пакет не опубликован:", holder.Name)
				continue
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(text), &doc); err != nil {
				allLift = false
				continue
			}
			allows := true
			for _, field := range []string{"dependencies", "peerDependencies", "optionalDependencies"} {
				deps, _ := doc[field].(map[string]any)
				if r, ok := deps[pkg].(string); ok {
					value, known := rangeAllows(override.Version, r)
					allows = allows && value && known
				}
			}
			if allows {
				fmt.Fprintln(out, "  обновление", holder.Name, doc["version"], "снимает ограничение")
			} else {
				allLift = false
			}
		}
		if allLift {
			actions++
			fmt.Fprintln(out, "  Обновите держателей и снимите override")
		}
	}
	if actions > 0 {
		return fmt.Errorf("overrides требуют изменений: %d", actions)
	}
	return nil
}
