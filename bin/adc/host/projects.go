package host

import (
	"encoding/json"
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func (a *App) ProjectCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("adc project add|list|rm|knowledge|relate")
	}
	if args[0] == "list" {
		c, e := a.shared().Load()
		if e != nil {
			return e
		}
		b, _ := json.MarshalIndent(c.Projects, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if e := a.shared().RequireHost(); e != nil {
		return e
	}
	return a.shared().Locked(func() error {
		c, e := a.shared().Load()
		if e != nil {
			return e
		}
		switch args[0] {
		case "add":
			if len(args) < 2 {
				return errors.New("adc project add <path> [--id ID] [--capability NAME]")
			}
			path, e := shared.Abs(args[1])
			if e != nil {
				return e
			}
			root, e := shared.Output(path, "git", "rev-parse", "--show-toplevel")
			if e != nil {
				return e
			}
			path, e = shared.Abs(root)
			if e != nil {
				return e
			}
			id := strings.ToLower(filepath.Base(path))
			id = shared.RegexpID(id) + "-" + shared.CheckoutID(path)
			p := shared.Project{ID: id, Path: path, Capabilities: []string{}}
			for i := 2; i < len(args); i++ {
				switch args[i] {
				case "--readonly":
					p.ReadOnly = true
				case "--id", "--capability", "--description":
					if i+1 == len(args) {
						return errors.New("отсутствует значение " + args[i])
					}
					key := args[i]
					i++
					switch key {
					case "--id":
						p.ID = args[i]
					case "--capability":
						p.Capabilities = append(p.Capabilities, args[i])
					case "--description":
						p.Description = args[i]
					}
				default:
					return fmt.Errorf("неизвестный флаг %s", args[i])
				}
			}
			if !shared.ValidID.MatchString(p.ID) {
				return errors.New("id: латинская буква, затем строчные буквы, цифры и дефисы; до 48 символов")
			}
			for _, existing := range c.Projects {
				if existing.ID == p.ID || existing.Path == p.Path {
					return errors.New("проект или id уже зарегистрирован")
				}
			}
			for _, existing := range c.Projects {
				if strings.HasPrefix(p.Path, existing.Path+string(os.PathSeparator)) || strings.HasPrefix(existing.Path, p.Path+string(os.PathSeparator)) {
					return errors.New("вложенные маунты проектов неоднозначны; зарегистрируйте отдельные корни")
				}
			}
			catalog, e := a.shared().DeliveryCatalog()
			if e != nil {
				return e
			}
			for _, cap := range p.Capabilities {
				if _, ok := catalog.Capabilities[cap]; !ok {
					return fmt.Errorf("неизвестная capability %s", cap)
				}
			}
			if runtime.GOOS == "windows" {
				info, e := os.Stat(filepath.Join(path, ".git"))
				if e != nil {
					return e
				}
				if !info.IsDir() {
					return errors.New("Windows linked worktree содержит хостовые Git пути; подключите основной checkout и создайте worktree внутри ADC")
				}
			}
			validation := p
			validation.RuntimePath = path // Team configuration is read on this host.
			if _, e = a.shared().ProjectCapabilities(validation); e != nil {
				return e
			}
			p.RuntimePath = shared.RuntimePath(path, p.ID)
			c.Projects = append(c.Projects, p)
			if e = a.shared().Save(c); e != nil {
				return e
			}
			fmt.Printf("Подключён %s → %s; adc workspace start применит маунты\n", p.ID, p.RuntimePath)
		case "rm":
			if len(args) != 2 {
				return errors.New("adc project rm <id>")
			}
			p, e := a.shared().Project(c, args[1])
			if e != nil {
				return e
			}
			kept := []shared.Project{}
			for _, v := range c.Projects {
				if v.ID != p.ID {
					for _, r := range v.Related {
						if r == p.ID {
							return errors.New("сначала удалите связь с " + v.ID + " в workspace.json")
						}
					}
					kept = append(kept, v)
				}
			}
			c.Projects = kept
			if e = a.shared().Save(c); e != nil {
				return e
			}
			fmt.Println("Регистрация удалена; код и worktree сохранены. adc workspace start применит изменения")
		case "knowledge":
			if len(args) != 4 {
				return errors.New("adc project knowledge <id|global> <relative-path> <origin>")
			}
			if strings.Contains(args[2], "\n") || strings.Contains(args[3], "\n") {
				return errors.New("перенос строки в metadata запрещён")
			}
			k := shared.Knowledge{Path: args[2], Origin: args[3], Updated: shared.Today()}
			if args[1] == "global" {
				if !strings.HasPrefix(k.Path, "/") {
					return errors.New("общий knowledge path должен быть абсолютным путём внутри зарегистрированного проекта")
				}
				inside := false
				for _, p := range c.Projects {
					if strings.HasPrefix(k.Path, p.RuntimePath+"/") {
						inside = true
					}
				}
				if !inside {
					return errors.New("источник знаний должен находиться в подключённом проекте")
				}
				c.Knowledge = append(c.Knowledge, k)
			} else {
				p, e := a.shared().Project(c, args[1])
				if e != nil {
					return e
				}
				if filepath.IsAbs(k.Path) || strings.HasPrefix(filepath.Clean(k.Path), "..") {
					return errors.New("project knowledge должен быть внутри репозитория")
				}
				for i := range c.Projects {
					if c.Projects[i].ID == p.ID {
						c.Projects[i].Knowledge = append(c.Projects[i].Knowledge, k)
					}
				}
			}
			return a.shared().Save(c)
		case "relate":
			if len(args) != 3 {
				return errors.New("adc project relate <id> <other-id>")
			}
			p, e := a.shared().Project(c, args[1])
			if e != nil {
				return e
			}
			q, e := a.shared().Project(c, args[2])
			if e != nil {
				return e
			}
			for i := range c.Projects {
				if c.Projects[i].ID == p.ID && !shared.Contains(c.Projects[i].Related, q.ID) {
					c.Projects[i].Related = append(c.Projects[i].Related, q.ID)
				}
			}
			return a.shared().Save(c)
		default:
			return errors.New("adc project add|list|rm|knowledge|relate")
		}
		return nil
	})
}
