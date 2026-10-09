package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type toolInstaller struct {
	home, tools, platform string
	run                   func(dir, name string, args ...string) ([]byte, error)
	fetch                 func(url string) ([]byte, error)
	out                   io.Writer
	graphify              bool
}

func replaceEnv(env []string, values map[string]string) []string {
	result := []string{}
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if _, ok := values[key]; !ok {
			result = append(result, item)
		}
	}
	for _, key := range sortedKeys(values) {
		result = append(result, key+"="+values[key])
	}
	return result
}

func toolsHome() string {
	if dir := os.Getenv("AI_TOOLS_HOME"); dir != "" {
		return dir
	}
	return "/opt/ai-tools"
}

func installerEnv(home, tools string) []string {
	// Detection must see actual CLIs, never ADC's agent adapters.
	path := strings.Join([]string{filepath.Join(tools, "bin"), filepath.Join(home, ".local", "bin"), filepath.Join(home, ".opencode", "bin"), "/usr/local/bin", "/usr/sbin", "/usr/bin", "/bin", "/usr/local/cargo/bin", "/usr/local/bun/bin"}, string(os.PathListSeparator))
	return replaceEnv(os.Environ(), map[string]string{"ADC_DELIVERY": "1", "PATH": path, "NPM_CONFIG_PREFIX": tools, "UV_INSTALL_DIR": filepath.Join(tools, "bin"), "INSTALLER_NO_MODIFY_PATH": "1", "GRAPHIFY_NO_AUTO_REFRESH": "1", "GRAPHIFY_NO_TIPS": "1", "UV_TOOL_BIN_DIR": filepath.Join(tools, "bin"), "UV_TOOL_DIR": filepath.Join(tools, "uv-tools")})
}

func findExecutable(name string, env []string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}
	path := ""
	for _, item := range env {
		if value, ok := strings.CutPrefix(item, "PATH="); ok {
			path = value
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s не установлен", name)
}

func fetchTool(url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("установка требует HTTPS")
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, response.StatusCode)
	}
	const limit = 256 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if len(data) > limit {
		return nil, errors.New("артефакт слишком большой")
	}
	return data, err
}

func (a *App) InstallTools() error {
	if err := a.shared().RequireRuntime(); err != nil {
		return err
	}
	return a.shared().Locked(func() error { return a.installTools() })
}

func (a *App) newToolInstaller() (*toolInstaller, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	env := installerEnv(home, toolsHome())
	i := toolInstaller{home: home, tools: toolsHome(), platform: a.Platform, out: os.Stdout, fetch: fetchTool}
	run := func(environment []string, dir, name string, args ...string) ([]byte, error) {
		binary, err := findExecutable(name, environment)
		if err != nil {
			return nil, err
		}
		timeout := 30 * time.Minute
		if len(args) == 1 && args[0] == "--version" {
			timeout = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir, cmd.Env = dir, environment
		if len(args) == 1 && args[0] == "--version" {
			return cmd.Output()
		}
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return nil, cmd.Run()
	}
	i.run = func(dir, name string, args ...string) ([]byte, error) { return run(env, dir, name, args...) }
	return &i, nil
}
func (a *App) installTools() error {
	i, err := a.newToolInstaller()
	if err != nil {
		return err
	}
	config, err := a.shared().Load()
	if err != nil {
		return err
	}
	for _, project := range config.Projects {
		caps, err := a.shared().ProjectCapabilities(project)
		if err != nil {
			return err
		}
		if !project.ReadOnly && shared.Contains(caps, "graphify") {
			i.graphify = true
		}
	}
	return i.install()
}
func (a *App) installGraphify() error {
	i, err := a.newToolInstaller()
	if err != nil {
		return err
	}
	return i.installGraphify()
}

func (i *toolInstaller) works(name string) bool {
	flag := "--version"
	if name == "skaffold" {
		flag = "version"
	}
	_, err := i.run(i.home, name, flag)
	return err == nil
}
func (i *toolInstaller) vendor(url, shell string) error {
	data, err := i.fetch(url)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "adc-vendor-*.sh")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	_, err = i.run(i.home, shell, file.Name())
	return err
}

func (i *toolInstaller) promote(name, source string) error {
	dest := filepath.Join(i.tools, "bin", name)
	if _, err := os.Lstat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if link, err := os.Readlink(source); err == nil {
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(source), link)
		}
		if err := replaceLink(dest, link); err != nil {
			return err
		}
	} else if err := copySkillFile(source, dest); err != nil {
		return err
	}
	return os.Remove(source)
}

func (i *toolInstaller) install() error {
	if err := os.MkdirAll(filepath.Join(i.tools, "bin"), 0755); err != nil {
		return err
	}
	for _, tool := range []struct{ name, url, source string }{
		{"claude", "https://claude.ai/install.sh", filepath.Join(i.home, ".local", "bin", "claude")},
		{"opencode", "https://opencode.ai/install", filepath.Join(i.home, ".opencode", "bin", "opencode")},
		{"hermes", "https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh", filepath.Join(i.home, ".local", "bin", "hermes")},
	} {
		if i.works(tool.name) {
			fmt.Fprintln(i.out, tool.name, "уже установлен")
			continue
		}
		fmt.Fprintln(i.out, "Установка", tool.name)
		if tool.name == "claude" {
			for _, path := range []string{tool.source, filepath.Join(i.tools, "bin", tool.name)} {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
		if tool.name == "hermes" {
			if err := os.RemoveAll(filepath.Join(i.home, ".hermes", "hermes-agent")); err != nil {
				return err
			}
		}
		if err := i.vendor(tool.url, "bash"); err != nil {
			return fmt.Errorf("%s: %w", tool.name, err)
		}
		if err := i.promote(tool.name, tool.source); err != nil {
			return err
		}
		if !i.works(tool.name) {
			return fmt.Errorf("%s установлен, но --version не работает", tool.name)
		}
	}
	if !i.works("codex") {
		if _, err := i.run(i.home, "npm", "install", "-g", "@openai/codex"); err != nil {
			return err
		}
		if !i.works("codex") {
			return errors.New("Codex не запускается после установки")
		}
	}
	if !i.works("dsh") {
		_, err := i.run(i.home, "npm", "install", "-g", "--allow-scripts=@deepseek-ai/dsh-subprocess-local,node-pty,koffi", "@deepseek-ai/dsh")
		if err != nil {
			_, err = i.run(i.home, "npm", "install", "-g", "@deepseek-ai/dsh")
		}
		if err != nil {
			fmt.Fprintln(i.out, "DSH не установлен:", err)
		}
	}
	if !i.works("uv") {
		if err := i.vendor("https://astral.sh/uv/install.sh", "sh"); err != nil {
			return err
		}
	}
	for _, tool := range []struct {
		name, pkg string
		required  bool
	}{{"strix", "strix-agent", false}} {
		if i.works(tool.name) {
			continue
		}
		_, _ = i.run(i.home, "uv", "tool", "uninstall", tool.pkg)
		_, err := i.run(i.home, "uv", "tool", "install", tool.pkg)
		if err == nil && !i.works(tool.name) {
			err = fmt.Errorf("%s не запускается после установки", tool.name)
		}
		if err != nil {
			if tool.required {
				return err
			}
			fmt.Fprintln(i.out, tool.name, "не установлен:", err)
		}
	}
	if i.graphify {
		if err := i.installGraphify(); err != nil {
			return err
		}
	}
	if !i.works("skaffold") {
		data, err := i.fetch("https://storage.googleapis.com/skaffold/releases/latest/skaffold-linux-" + runtime.GOARCH)
		if err != nil {
			return err
		}
		if err := shared.AtomicWrite(filepath.Join(i.tools, "bin", "skaffold"), data, 0755); err != nil {
			return err
		}
	}
	if err := i.installOmniRoute(); err != nil {
		return err
	}
	fmt.Fprintln(i.out, "Инструменты ADC готовы")
	return nil
}

func (i *toolInstaller) installGraphify() error {
	catalog, err := (&shared.App{Platform: i.platform}).DeliveryCatalog()
	if err != nil {
		return err
	}
	tools := catalog.Capabilities["graphify"].Tools
	if len(tools) != 1 || tools[0].Command != "graphify" {
		return errors.New("catalog: отсутствует поставка Graphify")
	}
	tool := tools[0]
	have, _ := i.run(i.home, "graphify", "--version")
	if strings.TrimPrefix(strings.TrimSpace(string(have)), "graphify ") == tool.Version {
		return nil
	}
	if !i.works("uv") {
		if err := i.vendor("https://astral.sh/uv/install.sh", "sh"); err != nil {
			return err
		}
	}
	if _, err := i.run(i.home, "uv", "tool", "install", "--force", tool.Package+"=="+tool.Version); err != nil {
		return fmt.Errorf("Graphify: %w", err)
	}
	have, err = i.run(i.home, "graphify", "--version")
	if err != nil || strings.TrimPrefix(strings.TrimSpace(string(have)), "graphify ") != tool.Version {
		return errors.New("Graphify не прошла проверку закреплённой версии")
	}
	return nil
}

func (i *toolInstaller) installOmniRoute() error {

	var lock struct {
		Version   int `json:"version"`
		OmniRoute struct {
			Version string `json:"version"`
			Scripts string `json:"scripts"`
		} `json:"omniroute"`
	}
	data, err := os.ReadFile(filepath.Join(i.platform, "runtime", "tools-lock.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}
	if lock.Version != 1 || !exactVersion.MatchString(lock.OmniRoute.Version) || lock.OmniRoute.Scripts == "" {
		return errors.New("некорректный tools-lock.json")
	}
	have, _ := i.run(i.home, "omniroute", "--version")
	if strings.TrimSpace(string(have)) != lock.OmniRoute.Version {
		if _, err := i.run(i.home, "npm", "install", "-g", "--allow-scripts="+lock.OmniRoute.Scripts, "omniroute@"+lock.OmniRoute.Version); err != nil {
			return fmt.Errorf("OmniRoute: %w", err)
		}
		version, err := i.run(i.home, "omniroute", "--version")
		if err != nil || strings.TrimSpace(string(version)) != lock.OmniRoute.Version {
			return errors.New("OmniRoute не прошёл проверку версии")
		}
	}
	return nil
}

func (a *App) InstallTool(name string) error {
	if err := a.shared().RequireRuntime(); err != nil {
		return err
	}
	return a.shared().Locked(func() error {
		i, err := a.newToolInstaller()
		if err != nil {
			return err
		}
		switch name {
		case "graphify":
			return i.installGraphify()
		case "omniroute":
			return i.installOmniRoute()
		default:
			return errors.New("инструмент должен быть graphify или omniroute")
		}
	})
}
