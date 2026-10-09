package runtime

import (
	"encoding/json"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) InstallMCPBrowsers(c shared.Config) error {
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return err
	}
	selected := shared.Contains(catalog.Base.MCP.Servers, "playwright") || shared.Contains(catalog.Base.MCP.Servers, "chrome-devtools")
	for _, p := range c.Projects {
		caps, err := a.shared().ProjectCapabilities(p)
		if err != nil {
			return err
		}
		for _, name := range caps {
			servers := catalog.Capabilities[name].MCP
			selected = selected || shared.Contains(servers, "playwright") || shared.Contains(servers, "chrome-devtools")
		}
	}
	if !selected {
		return nil
	}
	data, err := os.ReadFile(a.shared().DeliveryPath(catalog.Base.MCP.Source))
	if err != nil {
		return err
	}
	var definitions struct {
		Servers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err = json.Unmarshal(data, &definitions); err != nil {
		return err
	}
	pin := ""
	for _, arg := range definitions.Servers["playwright"].Args {
		if strings.HasPrefix(arg, "@playwright/mcp@") {
			pin = arg
		}
	}
	if pin == "" {
		return fmt.Errorf("в определении playwright отсутствует пин пакета MCP")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cache := os.Getenv("PLAYWRIGHT_BROWSERS_PATH")
	if cache == "" {
		cache = filepath.Join(home, ".cache", "ms-playwright")
	}
	installed, _ := os.ReadFile(filepath.Join(a.State, "mcp-browser-package"))
	if string(installed) == pin {
		if _, err := os.Stat(filepath.Join(cache, "chrome-current")); err == nil {
			return nil
		}
	}
	if err = shared.Run("/workspace", "npx", "-y", "--package="+pin, "playwright", "install", "chromium"); err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(cache, "chromium-*", "chrome-linux*", "chrome"))
	if err != nil {
		return err
	}
	latest := ""
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if latest == "" {
			latest = path
			continue
		}
		previous, err := os.Stat(latest)
		if err != nil || info.ModTime().After(previous.ModTime()) {
			latest = path
		}
	}
	if latest == "" {
		return fmt.Errorf("Playwright установлен, но полный Chromium не найден в %s", cache)
	}
	link := filepath.Join(cache, "chrome-current")
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s принадлежит пользователю", link)
		}
		if err = os.Remove(link); err != nil {
			return err
		}
	}
	if err = os.Symlink(latest, link); err != nil {
		return err
	}
	return shared.AtomicWrite(filepath.Join(a.State, "mcp-browser-package"), []byte(pin), 0644)
}
