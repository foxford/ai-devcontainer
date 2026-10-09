package runtime

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func (a *App) Helpers(dir string, args []string) error {
	if err := a.shared().RequireRuntime(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("adc tools --help")
	}
	if args[0] == "install" {
		if len(args) == 2 {
			return a.InstallTool(args[1])
		}
		if len(args) != 1 {
			return errors.New("adc tools install [graphify|omniroute]")
		}
		return a.InstallTools()
	}
	if args[0] == "claude-snapshot" {
		return a.shared().Locked(func() error { return ClaudeSnapshot(args[1:], os.Stdout) })
	}
	root, err := shared.Output(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	config, err := a.shared().Load()
	if err != nil {
		return err
	}
	p, err := a.FindCheckout(config, root)
	if err != nil {
		return err
	}
	if p.ReadOnly {
		return os.ErrPermission
	}
	switch args[0] {
	case "graphify":
		p.RuntimePath = root
		caps, err := a.shared().ProjectCapabilities(p)
		if err != nil {
			return err
		}
		if !shared.Contains(caps, "graphify") {
			return errors.New("подключите --capability graphify для проекта")
		}
		if len(args) == 2 && args[1] == "index" {
			return a.shared().Locked(func() error {
				if err := a.installGraphify(); err != nil {
					return err
				}
				return a.ensureGraph(root, true, os.Stderr)
			})
		}
		if len(args) == 2 && args[1] == "label-paths" {
			if err := a.shared().Locked(func() error { return a.ensureGraph(root, false, os.Stderr) }); err != nil {
				return err
			}
			return GraphLabels(root, graphRun, os.Stdout)
		}
		if len(args) == 3 && args[1] == "view" {
			port, err := strconv.Atoi(args[2])
			if err != nil {
				return err
			}
			if err := a.shared().Locked(func() error { return a.ensureGraph(root, false, os.Stderr) }); err != nil {
				return err
			}
			return GraphView(root, port)
		}
	case "pnpm-patch-dep":
		return a.shared().Locked(func() error {
			if len(args) == 2 && args[1] == "doctor" {
				return PatchDoctor(root, shared.Output, os.Stdout)
			}
			if len(args) < 2 || len(args) > 3 {
				return errors.New("adc tools pnpm-patch-dep <package> [version|major]")
			}
			version := ""
			if len(args) == 3 {
				version = args[2]
			}
			return PatchDependency(root, args[1], version, shared.Output, os.Stdout)
		})
	}
	return errors.New("adc tools --help")
}

func (a *App) OmniRoute(args []string) error {
	if err := a.shared().RequireRuntime(); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		args = []string{"--help"}
	}
	if args[0] == "serve" && !shared.Contains(args, "--no-open") {
		args = append(args, "--no-open")
	}
	cmd := exec.Command(filepath.Join(toolsHome(), "bin", "omniroute"), args...)
	// Run from home so a project's .env cannot alter the machine-wide gateway.
	cmd.Dir = home
	cmd.Env = replaceEnv(os.Environ(), map[string]string{"DATA_DIR": filepath.Join(home, ".omniroute"), "OMNIROUTE_HOST": "0.0.0.0", "OMNIROUTE_CLI_SKIP_REPO_ENV": "1"})
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
