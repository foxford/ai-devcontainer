package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func (a *App) Skill(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("adc skill --help")
	}
	layers, err := a.skills(root)
	if err != nil {
		return err
	}
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return err
	}
	stockRoot := a.shared().DeliveryPath(catalog.Base.Skills)
	projectRoot, baseRoot := filepath.Join(root, ".agents", "skills"), filepath.Join(root, ".ai-devcontainer", "skills-base")
	switch args[0] {
	case "list":
		fmt.Println("Скилл\tИсточник")
		for _, name := range skillNames(layers) {
			layer := layers[name]
			origin := "платформа"
			if layer.stock == "" {
				origin = "проект"
			} else if layer.project != "" {
				origin = "платформа + форк"
			}
			fmt.Printf("%s\t%s\n", name, origin)
		}
	case "status":
		for _, name := range skillNames(layers) {
			files, err := skillFiles(layers[name].project)
			if err != nil {
				return err
			}
			for _, rel := range sortedKeys(files) {
				stock, err := os.ReadFile(filepath.Join(stockRoot, name, rel))
				status := "своё (на платформе нет)"
				if err == nil {
					base, err := os.ReadFile(filepath.Join(baseRoot, name, rel))
					if os.IsNotExist(err) {
						status = "форк без базы — сверить вручную"
					} else if err != nil {
						return err
					} else if bytes.Equal(base, stock) {
						status = "форк актуален"
					} else {
						status = "платформа уехала"
					}
				} else if !os.IsNotExist(err) {
					return err
				}
				fmt.Printf("%s/%s\t%s\n", name, filepath.ToSlash(rel), status)
			}
		}
	case "fork", "unfork":
		if len(args) < 2 {
			return errors.New("нужно имя скилла")
		}
		name := args[1]
		if filepath.Base(name) != name || strings.ContainsAny(name, "\\:") || name == "." || name == ".." {
			return errors.New("некорректное имя скилла")
		}
		files := args[2:]
		if args[0] == "unfork" && len(files) == 0 {
			if err := os.RemoveAll(filepath.Join(projectRoot, name)); err != nil {
				return err
			}
			return os.RemoveAll(filepath.Join(baseRoot, name))
		}
		if len(files) == 0 {
			files = []string{"SKILL.md"}
		}
		// Validate the entire request before changing any files.
		for _, rel := range files {
			path, err := childPath(filepath.Join(stockRoot, name), rel)
			if err != nil {
				return err
			}
			if args[0] == "fork" {
				if _, err := os.Stat(path); err != nil {
					return err
				}
			}
		}
		for _, rel := range files {
			project, _ := childPath(filepath.Join(projectRoot, name), rel)
			base, _ := childPath(filepath.Join(baseRoot, name), rel)
			if args[0] == "fork" {
				if _, err := os.Lstat(project); err == nil {
					continue
				} else if !os.IsNotExist(err) {
					return err
				}
				stock, _ := childPath(filepath.Join(stockRoot, name), rel)
				if err := copySkillFile(stock, project); err != nil {
					return err
				}
				if err := copySkillFile(stock, base); err != nil {
					return err
				}
			} else {
				for _, path := range []string{project, base} {
					if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
						return err
					}
				}
			}
		}
	case "migrate":
		for _, name := range skillNames(layers) {
			files, err := skillFiles(layers[name].project)
			if err != nil {
				return err
			}
			for rel, project := range files {
				stock := filepath.Join(stockRoot, name, rel)
				want, err := os.ReadFile(stock)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return err
				}
				have, err := os.ReadFile(project)
				if err != nil {
					return err
				}
				if bytes.Equal(want, have) {
					if err := os.Remove(project); err != nil {
						return err
					}
				} else {
					base := filepath.Join(baseRoot, name, rel)
					if _, err := os.Stat(base); os.IsNotExist(err) {
						if err := copySkillFile(stock, base); err != nil {
							return err
						}
					}
				}
			}
		}
		documents, err := a.documents(pickAllCapabilities(catalog))
		if err != nil {
			return err
		}
		for _, doc := range documents {
			path, source := filepath.Join(root, doc.Target), a.shared().DeliveryPath(doc.Source)
			if ownedDocument(path, source) {
				continue
			}
			want, err := os.ReadFile(source)
			if err != nil {
				return err
			}
			have, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if bytes.Equal(want, have) {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}
	case "sync":
		return nil // Tool runs SyncProject once after mutations.
	default:
		return fmt.Errorf("неизвестная команда skill: %s", args[0])
	}
	return nil
}

func pickAllCapabilities(catalog shared.DeliveryCatalog) []string {
	return sortedKeys(catalog.Capabilities)
}
