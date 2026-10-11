package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type MCPOperation struct {
	Action     string   `json:"action"`
	Name       string   `json:"name,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	URL        string   `json:"url,omitempty"`
	Headers    []string `json:"headers,omitempty"`
	Definition string   `json:"definition,omitempty"`
	Command    []string `json:"command,omitempty"`
	JSON       bool     `json:"json,omitempty"`
}

func (a *App) scopeFile(root, scope string) (string, error) {
	switch scope {
	case "user":
		return filepath.Join(a.mcpStore(), "servers.json"), nil
	case "project":
		return filepath.Join(root, ".agents", "mcp.json"), nil
	case "local":
		return filepath.Join(root, ".agents", "mcp.local.json"), nil
	case "global":
		return "", errors.New("глобальный слой изменяется в delivery/catalog.json и delivery/mcp платформы")
	default:
		return "", fmt.Errorf("неизвестный слой %q", scope)
	}
}
func (a *App) secretFile(root, scope string) string {
	if scope == "user" {
		return filepath.Join(a.mcpStore(), "secrets.env")
	}
	return filepath.Join(root, ".agents", "mcp.secrets.env")
}

func setSecret(path, key, value string) error {
	if !envName.MatchString(key) || strings.ContainsAny(value, "\r\n") {
		return errors.New("некорректное имя или значение переменной секрета")
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	kept := []string{}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "export ") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		}
		candidate, _, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(candidate) == key {
			continue
		}
		if line != "" {
			kept = append(kept, line)
		}
	}
	// Values are literal data; readSecrets strips only these outer quotes.
	kept = append(kept, key+"=\""+value+"\"")
	return secureWrite(path, []byte(strings.Join(kept, "\n")+"\n"))
}

func writeLayer(layer MCPLayer) error {
	data, err := os.ReadFile(layer.File)
	var document map[string]any
	if err == nil {
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if document == nil {
		document = map[string]any{}
	}
	document["mcpServers"] = layer.Servers
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return secureWrite(layer.File, append(data, '\n'))
}

func extractSecrets(value any, server, key string, save func(string, string) error) (any, error) {
	switch value := value.(type) {
	case string:
		if !literalSecret(key, value) {
			return value, nil
		}
		variable := strings.ToUpper(server + "_" + key)
		var name strings.Builder
		for _, char := range variable {
			if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' {
				name.WriteRune(char)
			} else {
				name.WriteByte('_')
			}
		}
		variable = name.String()
		if variable == "" || variable[0] >= '0' && variable[0] <= '9' {
			variable = "MCP_" + variable
		}
		scheme, secret := "", value
		for _, prefix := range []string{"Bearer ", "Basic ", "Token "} {
			if strings.HasPrefix(value, prefix) {
				scheme, secret = prefix, strings.TrimSpace(strings.TrimPrefix(value, prefix))
				break
			}
		}
		if err := save(variable, secret); err != nil {
			return nil, err
		}
		return scheme + "${" + variable + "}", nil
	case map[string]any:
		result := map[string]any{}
		for _, key := range sortedKeys(value) {
			item, err := extractSecrets(value[key], server, key, save)
			if err != nil {
				return nil, err
			}
			result[key] = item
		}
		return result, nil
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			converted, err := extractSecrets(item, server, key, save)
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	default:
		return value, nil
	}
}

func (a *App) MCP(p shared.Project, operation MCPOperation) error {
	root := p.RuntimePath
	if operation.Action == "list" {
		plan, err := a.MCPPlan(p)
		if err != nil {
			return err
		}
		if operation.JSON {
			data, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}
		var rows [][]string
		var details []string
		for _, name := range sortedKeys(plan.Origins) {
			state, detail := "включён", ""
			if scope, ok := plan.Disabled[name]; ok {
				state, detail = "выключен", "отключён в "+scope
			}
			if reason, ok := plan.Reasons[name]; ok {
				state, detail = "недоступен", reason.Reason
			}
			rows = append(rows, []string{name, plan.Origins[name].Scope, state})
			if detail != "" {
				details = append(details, name+": "+detail)
			}
		}
		if err := shared.WriteTable(os.Stdout, []string{"Сервер", "Слой", "Статус"}, rows); err != nil {
			return err
		}
		if len(details) > 0 {
			fmt.Println("\nПодробности:")
			for _, detail := range details {
				fmt.Println("  " + detail)
			}
		}
		fmt.Printf("\nВсего серверов: %d. Статус описывает конфигурацию; соединения здесь не проверяются.\n", len(rows))
		return nil
	}
	if p.ReadOnly {
		return os.ErrPermission
	}
	if operation.Action == "sync" {
		return nil
	}
	if operation.Name != "" && !mcpServerName.MatchString(operation.Name) {
		return errors.New("имя MCP: A-Za-z0-9_- до 32 символов")
	}
	scope := operation.Scope
	if scope == "" {
		scope = "local"
		if operation.Action == "enable" {
			scope = "user"
		}
		if operation.Action == "fix-secrets" {
			scope = "project"
		}
	}
	if operation.Action == "rm" && operation.Scope == "" {
		found := false
		for _, candidate := range []string{"local", "project", "user"} {
			path, _ := a.scopeFile(root, candidate)
			layer, err := readLayer(candidate, path)
			if err != nil {
				return err
			}
			if _, ok := layer.Servers[operation.Name]; ok {
				scope = candidate
				found = true
				break
			}
		}
		if !found {
			return errors.New("сервер не найден в редактируемых слоях")
		}
	}
	path, err := a.scopeFile(root, scope)
	if err != nil {
		return err
	}
	layer, err := readLayer(scope, path)
	if err != nil {
		return err
	}
	switch operation.Action {
	case "add":
		var server map[string]any
		if operation.Definition != "" {
			if err := json.Unmarshal([]byte(operation.Definition), &server); err != nil {
				return fmt.Errorf("--json: %w", err)
			}
			if definitions, ok := server["mcpServers"].(map[string]any); ok {
				if operation.Name == "" && len(definitions) == 1 {
					operation.Name = sortedKeys(definitions)[0]
				}
				selected, ok := definitions[operation.Name].(map[string]any)
				if !ok {
					return errors.New("укажите имя сервера из JSON сниппета")
				}
				server = selected
			}
		} else if len(operation.Command) > 0 {
			server = map[string]any{"command": operation.Command[0], "args": operation.Command[1:]}
		} else if operation.URL != "" {
			server = map[string]any{"type": "http", "url": operation.URL}
			headers := map[string]any{}
			for _, header := range operation.Headers {
				key, value, ok := strings.Cut(header, ":")
				if !ok || strings.TrimSpace(key) == "" {
					return errors.New("--header: требуется Имя: значение")
				}
				headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
			if len(headers) > 0 {
				server["headers"] = headers
			}
		} else {
			return errors.New("требуется --json, --url или команда после --")
		}
		if server == nil || !mcpServerName.MatchString(operation.Name) {
			return errors.New("не указано корректное имя MCP сервера")
		}
		if scope == "project" {
			converted, err := extractSecrets(server, operation.Name, "", func(key, value string) error { return setSecret(a.secretFile(root, scope), key, value) })
			if err != nil {
				return err
			}
			server = converted.(map[string]any)
		}
		layer.Servers[operation.Name] = server
		return writeLayer(layer)
	case "rm":
		if _, ok := layer.Servers[operation.Name]; !ok {
			return errors.New("сервер отсутствует в выбранном слое")
		}
		delete(layer.Servers, operation.Name)
		return writeLayer(layer)
	case "disable":
		if operation.Name == "" {
			return errors.New("требуется имя сервера")
		}
		layer.Servers[operation.Name] = nil
		return writeLayer(layer)
	case "enable":
		for attempts := 0; attempts < 64; attempts++ {
			plan, err := a.MCPPlan(p)
			if err != nil {
				return err
			}
			if off, ok := plan.Disabled[operation.Name]; ok {
				file, err := a.scopeFile(root, off)
				if err != nil {
					return err
				}
				disabled, err := readLayer(off, file)
				if err != nil {
					return err
				}
				delete(disabled.Servers, operation.Name)
				if err := writeLayer(disabled); err != nil {
					return err
				}
				continue
			}
			if shared.Contains(plan.Active, operation.Name) {
				return nil
			}
			reason, ok := plan.Reasons[operation.Name]
			if !ok {
				return errors.New("сервер не выбран или отсутствует; adc mcp list")
			}
			if reason.NeedPath != "" {
				return fmt.Errorf("MCP требует путь: %s", reason.NeedPath)
			}
			if reason.NeedEnv == "" {
				return errors.New(reason.Reason)
			}
			if !isTerminal(os.Stdin) {
				fmt.Fprintln(os.Stderr, "Задайте", reason.NeedEnv, "в", a.secretFile(root, scope), "и выполните adc mcp sync")
				return nil
			}
			fmt.Fprint(os.Stderr, reason.NeedEnv+" = ")
			value, err := secretLine(os.Stdin)
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return err
			}
			if value == "" {
				return nil
			}
			if err := setSecret(a.secretFile(root, scope), reason.NeedEnv, value); err != nil {
				return err
			}
		}
		return errors.New("слишком много условий MCP")
	case "fix-secrets":
		for _, name := range sortedKeys(layer.Servers) {
			converted, err := extractSecrets(layer.Servers[name], name, "", func(key, value string) error { return setSecret(a.secretFile(root, scope), key, value) })
			if err != nil {
				return err
			}
			layer.Servers[name] = converted
		}
		return writeLayer(layer)
	default:
		return fmt.Errorf("неизвестная команда MCP %q", operation.Action)
	}
}
