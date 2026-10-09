package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"go.yaml.in/yaml/v3"
)

type mcpState struct {
	Version     int      `json:"version"`
	Servers     []string `json:"servers"`
	ClaudeOwned bool     `json:"claudeOwned"`
}

func secureWrite(path string, data []byte) error {
	if err := shared.AtomicWrite(path, data, 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func (a *App) ensureProjectSecrets(root string) error {
	path := filepath.Join(root, ".agents", "mcp.secrets.env")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(a.shared().DeliveryPath("mcp/secrets.env.example"))
	}
	if err != nil {
		return err
	}
	// AtomicWrite replaces a symlink instead of chmod/writing into its source.
	return secureWrite(path, data)
}
func readMCPState(root string) (mcpState, bool, error) {
	state := mcpState{Version: 1, Servers: []string{}}
	data, err := os.ReadFile(filepath.Join(root, ".ai-devcontainer", "mcp-state.json"))
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return state, false, err
		}
		if state.Version != 1 {
			return state, false, fmt.Errorf("неподдерживаемая версия MCP state")
		}
		return state, true, nil
	}
	if !os.IsNotExist(err) {
		return state, false, err
	}
	// Preserve ownership and stale names from the previous delivery engine.
	data, err = os.ReadFile(filepath.Join(root, ".ai-devcontainer", "mcp-state"))
	if err == nil {
		state.Servers = strings.Fields(string(data))
		state.ClaudeOwned = true
		return state, true, nil
	}
	if !os.IsNotExist(err) {
		return state, false, err
	}
	return state, false, nil
}

func yamlMapping(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var document yaml.Node
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if len(document.Content) == 0 || document.Content[0].Tag == "!!null" {
		document = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s должен содержать YAML объект", path)
	}
	return &document, nil
}
func yamlDelete(mapping *yaml.Node, key string) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}
func hermesMCP(path string, servers map[string]MCPServer, previous []string) ([]byte, error) {
	document, err := yamlMapping(path)
	if err != nil {
		return nil, err
	}
	mapping := document.Content[0]
	current := yamlField(mapping, "mcp_servers")
	if current == nil || current.Tag == "!!null" {
		current = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSet(mapping, "mcp_servers", current)
	}
	if current.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: mcp_servers должен быть объектом", path)
	}
	for _, name := range previous {
		if _, ok := servers[name]; !ok {
			yamlDelete(current, name)
		}
	}
	for _, name := range sortedKeys(servers) {
		data, err := yaml.Marshal(map[string]any(servers[name]))
		if err != nil {
			return nil, err
		}
		var server yaml.Node
		if err := yaml.Unmarshal(data, &server); err != nil {
			return nil, err
		}
		yamlSet(current, name, server.Content[0])
	}
	return yaml.Marshal(document)
}

func composeBlock(original, start, end, body string) (string, error) {
	begin, finish := strings.Index(original, start), strings.Index(original, end)
	if (begin >= 0) != (finish >= 0) || finish >= 0 && finish < begin {
		return "", fmt.Errorf("незавершённый управляемый блок %s", start)
	}
	block := ""
	if body != "" {
		block = start + "\n" + strings.TrimRight(body, "\n") + "\n" + end + "\n"
	}
	if begin >= 0 {
		return strings.TrimRight(original[:begin]+block+original[finish+len(end):], "\n") + "\n", nil
	}
	if block == "" {
		return original, nil
	}
	return strings.TrimRight(original, "\n") + "\n\n" + block, nil
}
func dshName(name string) string {
	if len(name) <= 32 {
		return name
	}
	return name[:23] + "-" + shared.CheckoutID(name)
}
func dshMCP(path, namespace, root string, servers map[string]MCPServer, oauth map[string]bool) ([]byte, error) {
	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	items := []map[string]any{}
	for _, name := range sortedKeys(servers) {
		if oauth[name] {
			continue
		}
		server := servers[name]
		config := map[string]any{"serverName": dshName(name)}
		if url, _ := server["url"].(string); url != "" {
			config["transport"] = "streamable-http"
			config["url"] = url
			if headers, ok := server["headers"]; ok {
				config["headers"] = headers
			}
		} else {
			command, _ := server["command"].(string)
			if command == "" {
				continue
			}
			config["transport"] = "stdio"
			config["command"] = command
			config["cwd"] = root
			config["args"] = []any{}
			if args, ok := server["args"]; ok {
				config["args"] = args
			}
			if env, ok := server["env"]; ok {
				config["env"] = env
			}
		}
		items = append(items, map[string]any{"insert": []map[string]any{{"id": "ai-devcontainer-mcp-" + name, "name": "@deepseek-ai/dsh-mcp-client", "config": config}}})
	}
	body := ""
	if len(items) > 0 {
		data, err := yaml.Marshal(items)
		if err != nil {
			return nil, err
		}
		body = string(data)
	}
	// Empty YAML list is a placeholder, not a list item to append patches to.
	text := string(original)
	var parsed any
	if err := yaml.Unmarshal(original, &parsed); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if list, ok := parsed.([]any); ok && len(list) == 0 {
		lines := strings.Split(text, "\n")
		kept := []string{}
		for _, line := range lines {
			if strings.TrimSpace(line) != "[]" {
				kept = append(kept, line)
			}
		}
		text = strings.Join(kept, "\n")
	}
	start, end := "# ai-devcontainer:mcp:"+namespace+":start", "# ai-devcontainer:mcp:"+namespace+":end"
	result, err := composeBlock(text, start, end, body)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal([]byte(result), &parsed); err != nil {
		return nil, fmt.Errorf("DSH patch не записан: %w", err)
	}
	if parsed != nil {
		if _, ok := parsed.([]any); !ok {
			return nil, fmt.Errorf("DSH patch должен содержать список")
		}
	}
	return []byte(result), nil
}

func codexEntryKind(path, name string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "none"
	}
	found, headers := false, false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			ours := section == "mcp_servers."+name || section == "mcp_servers.\""+name+"\"" || strings.HasPrefix(section, "mcp_servers."+name+".") || strings.HasPrefix(section, "mcp_servers.\""+name+"\".")
			if !ours {
				if found {
					break
				}
				continue
			}
			found = true
		}
		if found && strings.Contains(line, "http_headers") {
			headers = true
		}
	}
	if headers {
		return "headers"
	}
	if found {
		return "other"
	}
	return "none"
}

func syncCodex(servers map[string]MCPServer, previous []string, oauth map[string]bool) error {
	binary, err := exec.LookPath("codex")
	if err != nil {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	config := filepath.Join(home, ".codex", "config.toml")
	call := func(args ...string) error {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "ADC_DELIVERY=1")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Codex MCP: %w", err)
		}
		return nil
	}
	for _, name := range previous {
		if _, ok := servers[name]; !ok {
			if codexEntryKind(config, name) != "none" {
				if err := call("mcp", "remove", name); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range sortedKeys(servers) {
		server := servers[name]
		if oauth[name] {
			fmt.Fprintln(os.Stderr, "Codex: OAuth MCP", name, "добавляется и авторизуется вручную")
			continue
		}
		args := []string{"mcp", "add", name}
		if url, _ := server["url"].(string); url != "" {
			if headers, ok := server["headers"].(map[string]any); ok && len(headers) > 0 {
				kind := codexEntryKind(config, name)
				if kind == "other" {
					if err := call("mcp", "remove", name); err != nil {
						return err
					}
				}
				if kind != "headers" {
					fmt.Fprintf(os.Stderr, "Codex: MCP %s требует ручного блока http_headers в %s; значения берите из файла секретов\n", name, config)
				}
				continue
			}
			args = append(args, "--url", url)
		} else {
			if env, ok := server["env"].(map[string]any); ok {
				for _, key := range sortedKeys(env) {
					args = append(args, "--env", key+"="+fmt.Sprint(env[key]))
				}
			}
			command, _ := server["command"].(string)
			if command == "" {
				continue
			}
			args = append(args, "--", command)
			if values, ok := server["args"].([]any); ok {
				for _, value := range values {
					args = append(args, fmt.Sprint(value))
				}
			}
		}
		if err := call(args...); err != nil {
			return err
		}
	}
	if _, err := os.Stat(config); err == nil {
		return os.Chmod(config, 0600)
	}
	return nil
}

func (a *App) DeliverMCP(p shared.Project) error {
	if p.ReadOnly {
		return os.ErrPermission
	}
	if filepath.Clean(p.RuntimePath) == filepath.Clean(a.Platform) {
		return nil
	}
	plan, err := a.MCPPlan(p)
	if err != nil {
		return err
	}
	state, exists, err := readMCPState(p.RuntimePath)
	if err != nil {
		return err
	}
	namespace := p.ID + "-" + shared.CheckoutID(p.RuntimePath)
	servers := map[string]MCPServer{}
	oauth := map[string]bool{}
	for name, server := range plan.resolved {
		servers[namespace+"-"+name] = server
	}
	for _, name := range plan.OAuth {
		oauth[namespace+"-"+name] = true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	hermes := filepath.Join(home, ".hermes", "config.yaml")
	dsh := filepath.Join(home, ".dsh", "cordis.patch.yml")
	hermesData, err := hermesMCP(hermes, servers, state.Servers)
	if err != nil {
		return err
	}
	dshData, err := dshMCP(dsh, namespace, p.RuntimePath, servers, oauth)
	if err != nil {
		return err
	}
	claude := filepath.Join(p.RuntimePath, ".mcp.json")
	if !exists {
		if _, err := os.Lstat(claude); os.IsNotExist(err) {
			state.ClaudeOwned = true
		} else if err != nil {
			return err
		}
	}
	// Checkpoint ownership and the union of old/new names before any agent write.
	// If an external CLI fails, the next sync can retry and still remove stale names.
	pending := state
	names := map[string]bool{}
	for _, name := range state.Servers {
		names[name] = true
	}
	for name := range servers {
		names[name] = true
	}
	pending.Servers = sortedKeys(names)
	checkpoint, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return err
	}
	statePath := filepath.Join(p.RuntimePath, ".ai-devcontainer", "mcp-state.json")
	if err := secureWrite(statePath, append(checkpoint, '\n')); err != nil {
		return err
	}
	if err := a.ensureProjectSecrets(p.RuntimePath); err != nil {
		return err
	}
	if state.ClaudeOwned {
		if len(servers) == 0 {
			if err := os.Remove(claude); err != nil && !os.IsNotExist(err) {
				return err
			}
		} else {
			data, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
			if err != nil {
				return err
			}
			if err := secureWrite(claude, append(data, '\n')); err != nil {
				return err
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "MCP: пользовательский .mcp.json сохранён:", p.RuntimePath)
	}
	if err := secureWrite(hermes, hermesData); err != nil {
		return err
	}
	if err := secureWrite(dsh, dshData); err != nil {
		return err
	}
	if err := syncCodex(servers, state.Servers, oauth); err != nil {
		return err
	}
	state.Servers = sortedKeys(servers)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return secureWrite(statePath, append(data, '\n'))
}
