package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/foxford/ai-devcontainer/bin/adc/host"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	runtimecmd "github.com/foxford/ai-devcontainer/bin/adc/runtime"
)

// Select the execution side once. Runtime handlers never launch Docker;
// host handlers forward runtime work explicitly to the registered workspace.
func dispatch(args []string) error {
	ctx, err := shared.New()
	if err != nil {
		return err
	}
	h, r := (*host.App)(ctx), (*runtimecmd.App)(ctx)
	if len(args) == 0 {
		return errors.New("adc --help")
	}
	forward := func(dir string, tty bool) error { return h.Exec(dir, append([]string{"adc"}, args...), tty) }
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	switch args[0] {
	case "workspace":
		if ctx.Runtime {
			if len(args) == 2 && args[1] == "map" {
				return ctx.Locked(func() error {
					config, err := ctx.Load()
					if err != nil {
						return err
					}
					if err = r.WriteMap(config); err != nil {
						return err
					}
					fmt.Println("Карта проектов и знаний обновлена: /workspace/AGENTS.workspace.md")
					return nil
				})
			}
			return errors.New("управление Docker workspace доступно на хосте")
		}
		return h.Workspace(args[1:])
	case "project":
		if len(args) >= 3 && args[1] == "capability" {
			if args[2] == "list" {
				return ctx.WriteCapabilities(wd, os.Stdout, shared.ColorEnabled(os.Stdout))
			}
			if !ctx.Runtime && args[2] == "add" {
				if _, err := ctx.ChangeCapabilities(wd, args[2], args[3:]); err != nil {
					return err
				}
				if err := forward(wd, true); err != nil {
					return fmt.Errorf("выбор сохранён; настройка в runtime не завершена: %w; повторите adc project capability add после запуска workspace", err)
				}
				return nil
			}
			p, err := ctx.ChangeCapabilities(wd, args[2], args[3:])
			if err != nil {
				return err
			}
			fmt.Println("Возможности сохранены в .agents/project.json; синхронизация", p.ID)
			if ctx.Runtime {
				if args[2] == "add" {
					if err := r.ActivateCapabilities(p, args[3:]); err != nil {
						return fmt.Errorf("выбор сохранён; активация не завершена: %w; повторите capability add или adc mcp doctor --fix", err)
					}
				}
				err = r.Sync([]string{p.RuntimePath})
			} else {
				err = h.Exec(p.RuntimePath, []string{"adc", "sync"}, false)
			}
			if err != nil {
				return fmt.Errorf("настройки сохранены, доставка не завершена: %w; после исправления выполните adc sync", err)
			}
			return nil
		}
		return h.ProjectCommand(args[1:])
	case "sync":
		if ctx.Runtime {
			return r.Sync(args[1:])
		}
		dir := ""
		if len(args) == 1 {
			dir = wd
		}
		return forward(dir, false)
	case "graphify-mcp":
		if !ctx.Runtime || len(args) != 2 {
			return errors.New("graphify-mcp доступен только внутри ADC")
		}
		return r.GraphMCP(args[1], os.Stdin, os.Stdout)
	case "runtime-init":
		if !ctx.Runtime {
			return errors.New("runtime-init доступен только внутри ADC")
		}
		return r.Init()
	case "runtime-serve":
		return r.Serve()
	case "banner":
		if !ctx.Runtime {
			return errors.New("баннер доступен внутри ADC")
		}
		return runtimecmd.Banner(wd, os.Getenv("ADC_WORKSPACE"), os.Stdout)
	case "omniroute":
		if ctx.Runtime {
			return r.OmniRoute(args[1:])
		}
		return forward("", true)
	case "tools":
		if ctx.Runtime {
			return r.Helpers(wd, args[1:])
		}
		if len(args) > 1 && (args[1] == "claude-snapshot" || args[1] == "install") {
			return forward("", true)
		}
		return forward(wd, true)
	case "worktree":
		if ctx.Runtime {
			return r.Worktree(args[1:])
		}
		return forward("", false)
	case "agent":
		if ctx.Runtime {
			return r.Agent(args[1:])
		}
		return forward(wd, true)
	case "orca", "task":
		argv := []string{"orca"}
		if args[0] == "task" {
			argv = append(argv, "orchestration", "task")
		}
		argv = append(argv, args[1:]...)
		if ctx.Runtime {
			return shared.Run("", argv[0], argv[1:]...)
		}
		return h.Exec("", argv, false)
	case "migrate":
		if ctx.Runtime {
			return r.Migrate(args[1:])
		}
		return h.Migrate(args[1:])
	case "runtime-legacy-shared-archive":
		if !ctx.Runtime || len(args) != 2 {
			return errors.New("помощник архива доступен только внутри ADC")
		}
		return runtimecmd.LegacyArchive(args[1], os.Stdout, "share/mcp/servers.json", "share/mcp/secrets.env", "share/hermes/auth.json")
	case "runtime-legacy-archive":
		if !ctx.Runtime || len(args) != 2 {
			return errors.New("помощник архива доступен только внутри ADC")
		}
		return runtimecmd.LegacyArchive(args[1], os.Stdout)
	case "runtime-legacy-import":
		if !ctx.Runtime || len(args) != 3 {
			return errors.New("помощник импорта доступен только внутри ADC")
		}
		return runtimecmd.LegacyImportEnvironment(args[1], args[2])
	case "runtime-legacy-finish":
		if !ctx.Runtime || len(args) != 3 {
			return errors.New("помощник проверки доступен только внутри ADC")
		}
		return runtimecmd.VerifyLegacyImport("/home/node", args[1], args[2])
	case "restore":
		if ctx.Runtime {
			return r.Restore(args[1:])
		}
		return forward("", false)
	case "bootstrap-replace-legacy":
		return h.ReplaceLegacyInstallation(args[1])
	case "ensure-image":
		if err := ctx.RequireHost(); err != nil {
			return err
		}
		return h.EnsureImage()
	case "update":
		if ctx.Runtime {
			return r.Sync([]string{"--all"})
		}
		return h.Update()
	case "doctor":
		if ctx.Runtime {
			return r.Doctor()
		}
		return h.Doctor()
	case "skill", "mcp", "plans":
		if ctx.Runtime {
			return r.Tool(wd, args)
		}
		return forward(wd, true)
	case "runtime-tool":
		if !ctx.Runtime {
			return errors.New("runtime-tool доступен только внутри ADC")
		}
		return r.Tool(wd, args[1:])
	default:
		return fmt.Errorf("неизвестная команда %s; adc --help", args[0])
	}
}
