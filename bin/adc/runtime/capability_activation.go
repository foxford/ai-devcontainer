package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type activationInput func(shared.ActivationPrompt, string) (string, error)

func terminalActivationInput(prompt shared.ActivationPrompt, current string) (string, error) {
	if !isTerminal(os.Stdin) {
		return "", fmt.Errorf("нужен интерактивный терминал для %s; настройте .agents/mcp.local.json и .agents/mcp.secrets.env или запустите adc mcp doctor --fix", prompt.Label)
	}
	suffix := current
	if prompt.Secret && current != "" {
		suffix = "сохранён; Enter — оставить"
	}
	fmt.Fprintf(os.Stderr, "%s", prompt.Label)
	if suffix != "" {
		fmt.Fprintf(os.Stderr, " [%s]", suffix)
	}
	fmt.Fprint(os.Stderr, ": ")
	var value string
	var err error
	if prompt.Secret {
		value, err = secretLine(os.Stdin)
		fmt.Fprintln(os.Stderr)
	} else {
		value, err = bufio.NewReader(os.Stdin).ReadString('\n')
		value = strings.TrimRight(value, "\r\n")
	}
	if err != nil {
		return "", errors.New("ввод прерван; настройки не изменены")
	}
	if value == "" {
		value = current
	}
	if value == "" {
		return "", fmt.Errorf("%s не может быть пустым", prompt.Label)
	}
	return value, nil
}

func (a *App) activationVariables(p shared.Project) (map[string]string, error) {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for _, path := range []string{filepath.Join(a.mcpStore(), "secrets.env"), a.secretFile(p.RuntimePath, "project")} {
		extra, err := readSecrets(path)
		if err != nil {
			return nil, err
		}
		for key, value := range extra {
			values[key] = value
		}
	}
	return values, nil
}

func (a *App) configureActivation(p shared.Project, setup *shared.CapabilityActivation, force bool, input activationInput) error {
	if p.ReadOnly {
		return os.ErrPermission
	}
	if info, err := os.Lstat(filepath.Join(p.RuntimePath, ".agents")); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New(".agents — ссылка; используйте обычный каталог проекта")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	plan, err := a.MCPPlan(p)
	if err != nil {
		return err
	}
	if scope, ok := plan.Disabled[setup.Server]; ok {
		return fmt.Errorf("MCP %s выключен в %s; сначала adc mcp enable %s", setup.Server, scope, setup.Server)
	}
	if plan.Defs[setup.Server] == nil {
		return fmt.Errorf("MCP %s не выбран; добавьте соответствующую capability", setup.Server)
	}
	variables, err := a.activationVariables(p)
	if err != nil {
		return err
	}
	secrets := map[string]string{}
	address := ""
	changed := false
	printed := false
	for _, prompt := range setup.Prompts {
		current := variables[prompt.Key]
		needed := current == ""
		if prompt.Key == "url" {
			current, _ = plan.Defs[setup.Server]["url"].(string)
			// The platform default is a suggestion, not a verified host address.
			explicitURL := false
			for _, layer := range plan.Layers {
				if layer.Scope == "global" {
					continue
				}
				server, _ := layer.Servers[setup.Server].(map[string]any)
				address, _ := server["url"].(string)
				if address != "" {
					explicitURL = true
				}
			}
			needed = !explicitURL || current == ""
			if current == "" {
				current = prompt.Default
			}
		}
		if force || needed {
			if !printed {
				fmt.Fprintln(os.Stderr, "Для", setup.Server, "нужно:")
				for _, item := range setup.Requirements {
					fmt.Fprintln(os.Stderr, "  •", item)
				}
				printed = true
			}
			current, err = input(prompt, current)
			if err != nil {
				return err
			}
			changed = true
		}
		if prompt.Key == "url" {
			if err := validMCPURL(current); err != nil {
				return err
			}
			address = current
		} else {
			if current == "" || strings.ContainsAny(current, "\r\n") {
				return fmt.Errorf("некорректное значение %s", prompt.Key)
			}
			secrets[prompt.Key] = current
		}
	}
	if !changed {
		return nil
	}
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return err
	}
	var template MCPServer
	if err := json.Unmarshal(catalog.MCPDefinitions[setup.Server], &template); err != nil {
		return err
	}
	return a.shared().Locked(func() error {
		// Re-read the local layer after input: preserve concurrent unrelated changes.
		path, _ := a.scopeFile(p.RuntimePath, "local")
		layer, err := readLayer("local", path)
		if err != nil {
			return err
		}
		server, ok := layer.Servers[setup.Server].(map[string]any)
		if layer.Servers[setup.Server] == nil && layer.Exists {
			if _, disabled := layer.Servers[setup.Server]; disabled {
				return errors.New("сервер выключен во время настройки; повторите команду")
			}
		}
		if !ok {
			server = map[string]any{}
		}
		if address != "" {
			server["url"] = address
		}
		// Restore the bundle's bindings when a user explicitly configures its key.
		// Preserve unrelated headers/env instead of leaving a stale literal token.
		for _, field := range []string{"headers", "env"} {
			defaults, _ := template[field].(map[string]any)
			values, _ := server[field].(map[string]any)
			for name, raw := range defaults {
				text, ok := raw.(string)
				if !ok {
					continue
				}
				for key := range secrets {
					if strings.Contains(text, "${"+key+"}") {
						if values == nil {
							values = map[string]any{}
						}
						values[name] = text
					}
				}
			}
			if len(values) > 0 {
				server[field] = values
			}
		}
		for key, value := range secrets {
			if err := setSecret(a.secretFile(p.RuntimePath, "project"), key, value); err != nil {
				return err
			}
		}
		layer.Servers[setup.Server] = server
		if err := writeLayer(layer); err != nil {
			return err
		}
		return a.ignoreProject(p)
	})
}

func (a *App) ActivateCapabilities(p shared.Project, names []string) error {
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return err
	}
	for _, name := range names {
		setup := catalog.Capabilities[name].Activation
		if setup == nil {
			continue
		}
		if err := a.configureActivation(p, setup, false, terminalActivationInput); err != nil {
			return err
		}
		if err := a.MCPDoctor(p, MCPOperation{Action: "doctor", Name: setup.Server}); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) MCPDoctor(p shared.Project, operation MCPOperation) error {
	plan, err := a.MCPPlan(p)
	if err != nil {
		return err
	}
	if operation.Name != "" {
		if _, ok := plan.Origins[operation.Name]; !ok {
			return fmt.Errorf("MCP %s не найден в текущем проекте", operation.Name)
		}
	}
	if operation.Fix {
		if p.ReadOnly {
			return os.ErrPermission
		}
		catalog, err := a.shared().DeliveryCatalog()
		if err != nil {
			return err
		}
		caps, err := a.shared().ProjectCapabilities(p)
		if err != nil {
			return err
		}
		configured := map[string]bool{}
		for _, name := range caps {
			setup := catalog.Capabilities[name].Activation
			if setup == nil || (operation.Name != "" && operation.Name != setup.Server) {
				continue
			}
			if _, disabled := plan.Disabled[setup.Server]; disabled {
				continue
			}
			if err := a.configureActivation(p, setup, true, terminalActivationInput); err != nil {
				return err
			}
			configured[setup.Server] = true
		}
		for _, name := range sortedKeys(plan.Reasons) {
			reason := plan.Reasons[name]
			if configured[name] || (operation.Name != "" && operation.Name != name) || reason.NeedEnv == "" {
				continue
			}
			if err := a.configureMissingSecrets(p, name, terminalActivationInput); err != nil {
				return err
			}
		}
		// Delivery only: diagnostics do not rebuild Graphify indexes.
		if err := a.shared().Locked(func() error { return a.DeliverMCP(p) }); err != nil {
			return err
		}
		plan, err = a.MCPPlan(p)
		if err != nil {
			return err
		}
	}
	failures := 0
	checked := 0
	for _, name := range sortedKeys(plan.Origins) {
		if operation.Name != "" && name != operation.Name {
			continue
		}
		if scope, disabled := plan.Disabled[name]; disabled {
			fmt.Printf("— %s: выключен (%s)\n", name, scope)
			if operation.Name != "" {
				failures++
			}
			continue
		}
		checked++
		if reason, blocked := plan.Reasons[name]; blocked {
			fmt.Printf("✗ %s: %s; adc mcp doctor %s --fix\n", name, reason.Reason, name)
			failures++
			continue
		}
		if shared.Contains(plan.OAuth, name) {
			fmt.Printf("✗ %s: требуется авторизация OAuth в агенте\n", name)
			failures++
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		fmt.Fprintf(os.Stderr, "Проверка MCP: %s…\n", name)
		count, err := probeMCP(ctx, p.RuntimePath, plan.resolved[name])
		cancel()
		if err != nil {
			fmt.Printf("✗ %s: %s\n", name, err)
			failures++
		} else {
			fmt.Printf("✓ %s: initialize и tools/list OK; инструментов: %d\n", name, count)
		}
	}
	fmt.Printf("Проверено серверов: %d; ошибок: %d. Инструменты не вызывались.\n", checked, failures)
	if failures > 0 {
		return fmt.Errorf("MCP doctor: ошибок %d; исправьте настройки и повторите проверку", failures)
	}
	return nil
}

func (a *App) configureMissingSecrets(p shared.Project, name string, input activationInput) error {
	if p.ReadOnly {
		return os.ErrPermission
	}
	if info, err := os.Lstat(filepath.Join(p.RuntimePath, ".agents")); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New(".agents — ссылка; используйте обычный каталог проекта")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	seen := map[string]bool{}
	for {
		plan, err := a.MCPPlan(p)
		if err != nil {
			return err
		}
		reason := plan.Reasons[name]
		if reason.NeedEnv == "" {
			return nil
		}
		if seen[reason.NeedEnv] {
			return fmt.Errorf("MCP %s: значение %s не удовлетворяет условиям", name, reason.NeedEnv)
		}
		seen[reason.NeedEnv] = true
		value, err := input(shared.ActivationPrompt{Key: reason.NeedEnv, Label: reason.NeedEnv, Secret: true}, "")
		if err != nil {
			return err
		}
		if value == "" {
			return fmt.Errorf("%s не может быть пустым", reason.NeedEnv)
		}
		if err := a.shared().Locked(func() error {
			if err := setSecret(a.secretFile(p.RuntimePath, "project"), reason.NeedEnv, value); err != nil {
				return err
			}
			return a.ignoreProject(p)
		}); err != nil {
			return err
		}
	}
}
