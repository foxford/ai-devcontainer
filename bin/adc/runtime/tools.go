package runtime

import (
	"encoding/json"
	"errors"
	"os"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func (a *App) Tool(dir string, args []string) error {
	if len(args) == 0 {
		return errors.New("отсутствует команда runtime")
	}
	// Probes and interactive input must never hold the shared delivery lock.
	if args[0] == "mcp" && len(args) == 2 {
		var operation MCPOperation
		if err := json.Unmarshal([]byte(args[1]), &operation); err != nil {
			return err
		}
		if operation.Action == "doctor" {
			p, err := a.shared().CapabilityProject(dir)
			if err != nil {
				return err
			}
			return a.MCPDoctor(p, operation)
		}
	}
	var indexed *shared.Project
	err := a.shared().Locked(func() error {
		config, err := a.shared().Load()
		if err != nil {
			return err
		}
		root, err := shared.Output(dir, "git", "rev-parse", "--show-toplevel")
		if err != nil {
			return err
		}
		project, err := a.FindCheckout(config, root)
		if err != nil {
			return err
		}
		project.RuntimePath = root
		mutates := false
		switch args[0] {
		case "plans":
			return Plans(root, shared.Contains(args[1:], "--all"))
		case "skill":
			if len(args) < 2 {
				return errors.New("adc skill --help")
			}
			mutates = shared.Contains([]string{"fork", "unfork", "migrate", "sync"}, args[1])
			if project.ReadOnly && mutates {
				return os.ErrPermission
			}
			if err := a.Skill(root, args[1:]); err != nil {
				return err
			}
		case "mcp":
			if len(args) != 2 {
				return errors.New("отсутствует описание команды MCP")
			}
			var operation MCPOperation
			if err := json.Unmarshal([]byte(args[1]), &operation); err != nil {
				return err
			}
			mutates = operation.Action != "list"
			if project.ReadOnly && mutates {
				return os.ErrPermission
			}
			if err := a.MCP(project, operation); err != nil {
				return err
			}
		default:
			return errors.New("неизвестная команда runtime")
		}
		if mutates {
			if err := a.SyncProject(project); err != nil {
				return err
			}
			indexed = &project
		}
		return nil
	})
	if err == nil && indexed != nil {
		a.syncGraph(*indexed)
	}
	return err
}
