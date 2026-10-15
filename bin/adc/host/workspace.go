package host

import (
	"context"
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *App) Workspace(args []string) error {
	if len(args) == 0 {
		return errors.New("adc workspace --help")
	}
	if args[0] == "config" {
		return a.WorkspaceConfig(args[1:], os.Stdout)
	}
	if args[0] == "init" {
		if e := a.shared().RequireHost(); e != nil {
			return e
		}
		return a.shared().Locked(func() error {
			if _, e := os.Stat(filepath.Join(a.State, "workspace.json")); e == nil {
				return errors.New("workspace уже существует")
			}
			c := shared.Config{Version: shared.Schema, Name: "developer", Port: shared.DefaultOrcaPort, Projects: []shared.Project{}}
			for i := 1; i < len(args); i++ {
				if shared.Contains([]string{"--port", "--omniroute-port", "--graph-port"}, args[i]) {
					flag := args[i]
					i++
					if i == len(args) {
						return errors.New("нет порта")
					}
					n, e := strconv.Atoi(args[i])
					if e != nil {
						return e
					}
					switch flag {
					case "--port":
						c.Port = n
					case "--omniroute-port":
						c.OmniRoutePort = n
					case "--graph-port":
						c.GraphPort = n
					}
				} else {
					c.Name = args[i]
				}
			}
			if !shared.ValidID.MatchString(c.Name) || c.Port < 1 || c.Port > 65535 {
				return errors.New("неверное имя или порт")
			}
			if err := c.ValidatePorts(); err != nil {
				return err
			}
			if e := a.shared().Save(c); e != nil {
				return e
			}
			fmt.Println("Workspace:", filepath.Join(a.State, "workspace.json"))
			return nil
		})
	}
	c, e := a.shared().Load()
	if e != nil {
		return e
	}
	switch args[0] {
	case "start":
		if e = a.shared().RequireHost(); e != nil {
			return e
		}
		if shared.IsTerminalOutput(os.Stdout) {
			a.shared().ShowUpdateNotice(os.Stdout)
		}
		return a.Start(c)
	case "stop":
		if e = a.shared().RequireHost(); e != nil {
			return e
		}
		return a.shared().LockedFile("lifecycle.lockfile", func() error {
			return shared.Run("", "docker", "stop", a.shared().Container(c))
		})
	case "status":
		if e = a.shared().RequireHost(); e != nil {
			return e
		}
		if pending, err := a.readReplacement(c); err != nil {
			return err
		} else if pending != nil {
			fmt.Println("Переключение контейнера не завершено; резервное имя:", pending.Backup, "; продолжить: adc workspace start")
		}
		state, err := dockerStartup(context.Background(), a.shared().Container(c))
		if err != nil {
			return err
		}
		fmt.Println(startupSummary(state))
		return nil
	case "logs":
		if e = a.shared().RequireHost(); e != nil {
			return e
		}
		return runWorkspaceLogs(a.shared().Container(c), args[1:], shared.Run)
	case "shell":
		if shared.IsTerminalOutput(os.Stdout) {
			a.shared().ShowUpdateNotice(os.Stdout)
		}
		dir := "/workspace"
		if len(args) > 1 {
			p, e := a.shared().Project(c, args[1])
			if e != nil {
				return e
			}
			dir = p.RuntimePath
		}
		return a.Exec(dir, []string{"zsh"}, true)
	case "map":
		return a.Exec("", []string{"adc", "workspace", "map"}, false)
	default:
		return errors.New("adc workspace --help")
	}
}
func mount(source, target string, ro bool) (string, error) {
	if strings.ContainsAny(source, ",\n\r") || strings.ContainsAny(target, ",\n\r") {
		return "", errors.New("Docker bind path содержит недопустимый разделитель")
	}
	s := "type=bind,src=" + source + ",dst=" + target
	if ro {
		s += ",readonly"
	}
	return s, nil
}
func (a *App) DockerArgs(c shared.Config) ([]string, error) {
	if err := c.ValidatePorts(); err != nil {
		return nil, err
	}
	args := []string{"run", "-d", "--init", "--name", a.shared().Container(c), "--label", "adc.workspace=" + c.Name, "--restart", "unless-stopped", "-p", fmt.Sprintf("127.0.0.1:%d:6768", c.Port), "--add-host", "host.docker.internal:host-gateway", "-e", "ADC_RUNTIME=1", "-e", fmt.Sprintf("ADC_ORCA_PORT=%d", c.Port), "-e", "ADC_HOME=/var/lib/adc", "-e", "AI_DEVCONTAINER_HOME=/opt/ai-devcontainer", "-e", "LIBGL_ALWAYS_SOFTWARE=1", "--mount", "type=volume,src=adc-" + c.Name + "-home,dst=/home/node", "--mount", "type=volume,src=adc-" + c.Name + "-tools,dst=/opt/ai-tools", "--mount", "type=volume,src=adc-" + c.Name + "-worktrees,dst=/workspace"}
	args = append(args, "--security-opt", "seccomp="+a.dockerSeccompPath())
	args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:20128", c.GatewayPort()), "-p", fmt.Sprintf("127.0.0.1:%d:4242", c.PreviewPort()))
	for _, pair := range [][2]string{{a.Platform, "/opt/ai-devcontainer"}, {a.State, "/var/lib/adc"}} {
		v, e := mount(pair[0], pair[1], pair[1] == "/opt/ai-devcontainer")
		if e != nil {
			return nil, e
		}
		args = append(args, "--mount", v)
	}
	for _, p := range c.Projects {
		v, e := mount(p.Path, p.RuntimePath, p.ReadOnly)
		if e != nil {
			return nil, e
		}
		args = append(args, "--mount", v)
	}
	args = append(args, "-e", "ADC_WORKSPACE="+c.Name)
	// Host UID avoids root-owned edits on Linux bind mounts. No host home or socket.
	if uid := os.Getuid(); uid > 0 {
		args = append(args, "-e", fmt.Sprintf("ADC_UID=%d", uid), "-e", fmt.Sprintf("ADC_GID=%d", os.Getgid()))
	}
	if value := os.Getenv("ADC_INSTALL_AI_TOOLS"); value != "" {
		args = append(args, "-e", "ADC_INSTALL_AI_TOOLS="+value)
	}
	args = append(args, "adc-runtime:local")
	return args, nil
}
func (a *App) EnsureImage() error {
	// Legacy import may already hold write.lockfile. Keep this build helper free
	// of host lifecycle locks; Update and Start acquire their locks explicitly.
	_, err := a.ensureImage()
	return err
}

func (a *App) ensureImage() (string, error) {
	if _, e := shared.Output("", "docker", "info", "--format", "{{.OSType}}"); e != nil {
		return "", fmt.Errorf("Docker недоступен; adc install: %w", e)
	}
	if err := os.MkdirAll(a.State, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(a.State, ".adc-image-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if err := file.Close(); err != nil {
		return "", err
	}
	// BuildKit writes the ID of this build even if another ADC installation
	// changes the shared local tag before container creation.
	if err := shared.Run("", "docker", "build", "--builder", "default", "--iidfile", file.Name(), "-f", filepath.Join(a.Platform, "runtime", "Dockerfile"), "--build-arg", "ADC_REVISION="+shared.SourceRevision(a.Platform), "-t", "adc-runtime:local", a.Platform); err != nil {
		return "", err
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
func (a *App) Start(c shared.Config) error {
	return a.shared().LockedFile("update.lockfile", func() error {
		return a.shared().LockedFile("lifecycle.lockfile", func() error {
			if err := a.checkMigration(); err != nil {
				return err
			}
			// A config change may have completed while start waited for this lock.
			var err error
			c, err = a.shared().Load()
			if err != nil {
				return err
			}
			if resumed, err := a.resumeReplacement(c, realDocker(), a.replacementWait(c)); resumed || err != nil {
				return err
			}
			image, err := a.ensureImage()
			if err != nil {
				return err
			}
			prepared, err := a.prepareWorkspace(c, image)
			if err != nil {
				return err
			}
			return a.launchWorkspace(prepared, realDocker(), a.replacementWait(c))
		})
	})
}

func (a *App) checkMigration() error {
	if _, err := os.Stat(filepath.Join(a.State, "migration-pending.json")); !os.IsNotExist(err) {
		return errors.New("миграция не завершена: adc migrate legacy finish; запуск среды заблокирован")
	}
	return nil
}
func (a *App) Exec(dir string, argv []string, tty bool) error {
	if e := a.shared().RequireHost(); e != nil {
		return e
	}
	c, e := a.shared().Load()
	if e != nil {
		return e
	}
	if dir != "" && dir != "/workspace" {
		mapped := false
		for _, p := range c.Projects {
			if dir == p.RuntimePath || strings.HasPrefix(dir, p.RuntimePath+"/") {
				mapped = true
				break
			}
			if dir == p.Path || strings.HasPrefix(dir, p.Path+string(os.PathSeparator)) {
				rel, _ := filepath.Rel(p.Path, dir)
				dir = p.RuntimePath
				if rel != "." {
					dir += "/" + filepath.ToSlash(rel)
				}
				mapped = true
				break
			}
		}
		if !mapped {
			return errors.New("текущий каталог не подключён: adc project add")
		}
	}
	args := []string{"exec", "-i", "--user", "node"}
	if input, err := os.Stdin.Stat(); tty && err == nil && input.Mode()&os.ModeCharDevice != 0 {
		args = append(args, "-t")
	}
	if dir != "" {
		args = append(args, "--workdir", dir)
	}
	args = append(args, a.shared().Container(c))
	args = append(args, argv...)
	return shared.Run("", "docker", args...)
}
func (a *App) Doctor() error {
	fmt.Println("Платформа:", a.Platform)
	fmt.Println("Состояние:", a.State)

	for _, name := range []string{"git", "docker"} {
		if _, e := shared.Output("", name, "--version"); e != nil {
			return e
		}
	}
	if _, e := shared.Output("", "docker", "info", "--format", "{{.OSType}}"); e != nil {
		return e
	}
	c, e := a.shared().Load()
	if e != nil {
		return e
	}
	return shared.Run("", "docker", "inspect", "--format", "{{.State.Status}}", a.shared().Container(c))
}
