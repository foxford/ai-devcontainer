package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) Workspace(args []string) error {
	if len(args) == 0 {
		return errors.New("adc workspace --help")
	}
	if args[0] == "init" {
		if e := a.shared().RequireHost(); e != nil {
			return e
		}
		return a.shared().Locked(func() error {
			if _, e := os.Stat(filepath.Join(a.State, "workspace.json")); e == nil {
				return errors.New("workspace уже существует")
			}
			c := shared.Config{Version: shared.Schema, Name: "developer", Port: 6768, Projects: []shared.Project{}}
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
		return a.Start(c)
	case "stop":
		if e = a.shared().RequireHost(); e != nil {
			return e
		}
		return shared.Run("", "docker", "stop", a.shared().Container(c))
	case "status":
		if e = a.shared().RequireHost(); e != nil {
			return e
		}
		return shared.Run("", "docker", "inspect", "--format", "{{.State.Status}}", a.shared().Container(c))
	case "logs":
		if e = a.shared().RequireHost(); e != nil {
			return e
		}
		return shared.Run("", "docker", "logs", "--tail", "100", a.shared().Container(c))
	case "shell":
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
	if _, e := shared.Output("", "docker", "info", "--format", "{{.OSType}}"); e != nil {
		return fmt.Errorf("Docker недоступен; adc install: %w", e)
	}
	return shared.Run("", "docker", "build", "--builder", "default", "-f", filepath.Join(a.Platform, "runtime", "Dockerfile"), "--build-arg", "ADC_REVISION="+shared.SourceRevision(a.Platform), "-t", "adc-runtime:local", a.Platform)
}
func (a *App) Start(c shared.Config) error {
	if _, err := os.Stat(filepath.Join(a.State, "migration-pending.json")); !os.IsNotExist(err) {
		return errors.New("миграция не завершена: adc migrate legacy finish; запуск среды заблокирован")
	}
	if e := a.EnsureImage(); e != nil {
		return e
	}
	args, e := a.DockerArgs(c)
	if e != nil {
		return e
	}
	b, _ := json.Marshal(args)
	h := sha256.Sum256(b)
	image, e := shared.Output("", "docker", "image", "inspect", "--format", "{{.Id}}", "adc-runtime:local")
	if e != nil {
		return e
	}
	desired := hex.EncodeToString(h[:]) + image
	recorded, _ := os.ReadFile(filepath.Join(a.State, "container-key"))
	status, e := shared.Output("", "docker", "inspect", "--format", "{{.State.Status}}", a.shared().Container(c))
	if e == nil && string(recorded) == desired {
		if status != "running" {
			if e = shared.Run("", "docker", "start", a.shared().Container(c)); e != nil {
				return e
			}
		}
	} else {
		if e == nil {
			if e = shared.Run("", "docker", "rm", "-f", a.shared().Container(c)); e != nil {
				return e
			}
		}
		if e = shared.Run("", "docker", args...); e != nil {
			return e
		}
		if e = shared.AtomicWrite(filepath.Join(a.State, "container-key"), []byte(desired), 0600); e != nil {
			return e
		}
	}
	fmt.Println("Ожидание доставки контекста и Orca...")
	for deadline := time.Now().Add(10 * time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
		if _, e = shared.Output("", "docker", "exec", a.shared().Container(c), "test", "-f", "/tmp/adc-ready"); e == nil {
			fmt.Printf("Workspace запущен: Orca → Add Server → 127.0.0.1:%d. Pairing: adc orca pairing --help или adc workspace logs\n", c.Port)
			return nil
		}
		status, e = shared.Output("", "docker", "inspect", "--format", "{{.State.Status}}", a.shared().Container(c))
		if e != nil || status != "running" {
			return fmt.Errorf("runtime не запущен; adc workspace logs: %s", status)
		}
	}
	return errors.New("runtime ещё не готов; adc workspace logs")
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
