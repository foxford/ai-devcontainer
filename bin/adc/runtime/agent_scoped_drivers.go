package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"go.yaml.in/yaml/v3"
)

type hermesDriver struct{ defaultAgentBehavior }

func (hermesDriver) Name() string { return "hermes" }

func hermesManagedPath(root string) string {
	return filepath.Join(root, shared.ProjectStateDir, "hermes-managed")
}

const hermesManagedMarker = "ai-devcontainer:hermes-managed:v1"

type hermesManagedSource struct {
	Marker string `json:"marker"`
	Source string `json:"source"`
}

// A nested agent inherits its parent's managed overlay. Resolve the original
// policy rather than importing another checkout's MCP into the new overlay.
func hermesPolicySource() (string, error) {
	source := os.Getenv("HERMES_MANAGED_DIR")
	if source == "" {
		source = "/etc/hermes"
	}
	seen := map[string]bool{}
	for {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		if seen[absolute] {
			return "", fmt.Errorf("циклический источник управляемой конфигурации Hermes: %s", absolute)
		}
		seen[absolute] = true
		data, err := os.ReadFile(filepath.Join(absolute, "adc-source.json"))
		if os.IsNotExist(err) {
			return absolute, nil
		}
		if err != nil {
			return "", err
		}
		var metadata hermesManagedSource
		if err := json.Unmarshal(data, &metadata); err != nil {
			return "", err
		}
		if metadata.Marker != hermesManagedMarker {
			return absolute, nil
		}
		if metadata.Source == "" {
			return "", fmt.Errorf("пустой источник управляемой конфигурации Hermes: %s", absolute)
		}
		source = metadata.Source
	}
}

func configuredAgentHome(home, variable, directory string) string {
	if value := os.Getenv(variable); value != "" {
		return value
	}
	return filepath.Join(home, directory)
}

func (hermesDriver) Prepare(d agentDelivery) (agentPreparation, error) {
	homeConfig := filepath.Join(configuredAgentHome(d.Home, "HERMES_HOME", ".hermes"), "config.yaml")
	legacy := append([]string{}, d.Previous.Servers...)
	for _, checkout := range d.Registry {
		legacy = append(legacy, checkout.State.Servers...)
	}
	cleaned, err := hermesMCP(homeConfig, nil, legacy)
	if err != nil {
		return agentPreparation{}, err
	}
	source, err := hermesPolicySource()
	if err != nil {
		return agentPreparation{}, err
	}
	policy, err := yamlMapping(filepath.Join(source, "config.yaml"))
	if err != nil {
		return agentPreparation{}, err
	}
	var policyValues map[string]any
	if err := policy.Decode(&policyValues); err != nil {
		return agentPreparation{}, err
	}
	servers := map[string]any{}
	for name, server := range d.Servers {
		servers[name] = map[string]any(server)
	}
	// Existing administrator policy keeps native Hermes precedence, including
	// individual fields of a server also supplied by ADC.
	config, err := yaml.Marshal(mergeObject(map[string]any{"mcp_servers": servers}, policyValues))
	if err != nil {
		return agentPreparation{}, err
	}
	environment, err := os.ReadFile(filepath.Join(source, ".env"))
	hasEnvironment := err == nil
	if err != nil && !os.IsNotExist(err) {
		return agentPreparation{}, err
	}
	metadata, err := json.Marshal(hermesManagedSource{Marker: hermesManagedMarker, Source: source})
	if err != nil {
		return agentPreparation{}, err
	}
	destination := hermesManagedPath(d.Project.RuntimePath)
	if filepath.Clean(source) == filepath.Clean(destination) {
		return agentPreparation{}, fmt.Errorf("источник конфигурации Hermes совпадает с каталогом доставки: %s", source)
	}
	return agentPreparation{Commit: func() error {
		if err := secureWrite(filepath.Join(destination, "config.yaml"), config); err != nil {
			return err
		}
		if err := secureWrite(filepath.Join(destination, "adc-source.json"), append(metadata, '\n')); err != nil {
			return err
		}
		envPath := filepath.Join(destination, ".env")
		if hasEnvironment {
			if err := secureWrite(envPath, environment); err != nil {
				return err
			}
		} else if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return secureWrite(homeConfig, cleaned)
	}}, nil
}

func (hermesDriver) Launch(root string, args []string, administrative bool) ([]string, map[string]string, error) {
	if administrative {
		return args, nil, nil
	}
	path := hermesManagedPath(root)
	if _, err := os.Stat(filepath.Join(path, "config.yaml")); err != nil {
		return nil, nil, err
	}
	return args, map[string]string{"HERMES_MANAGED_DIR": path}, nil
}

type dshDriver struct{ defaultAgentBehavior }

func (dshDriver) Name() string { return "dsh" }

func dshScopedPath(root string) string {
	return filepath.Join(root, shared.ProjectStateDir, "dsh-mcp.patch.yml")
}

func (dshDriver) Prepare(d agentDelivery) (agentPreparation, error) {
	homeConfig := filepath.Join(configuredAgentHome(d.Home, "DSH_HOME", ".dsh"), "cordis.patch.yml")
	cleaned, err := dshMCP(homeConfig, d.Namespace, d.Project.RuntimePath, nil, nil)
	if err != nil {
		return agentPreparation{}, err
	}
	for _, checkout := range d.Registry {
		namespace := checkout.Namespace
		if namespace == d.Namespace {
			continue
		}
		result, err := composeBlock(string(cleaned), "# ai-devcontainer:mcp:"+namespace+":start", "# ai-devcontainer:mcp:"+namespace+":end", "")
		if err != nil {
			return agentPreparation{}, err
		}
		cleaned = []byte(result)
	}
	var patches any
	if err := yaml.Unmarshal(cleaned, &patches); err != nil {
		return agentPreparation{}, fmt.Errorf("DSH patch не записан: %w", err)
	}
	path := dshScopedPath(d.Project.RuntimePath)
	config, err := dshMCP(path, d.Namespace, d.Project.RuntimePath, d.Servers, d.OAuth)
	if err != nil {
		return agentPreparation{}, err
	}
	if strings.TrimSpace(string(config)) == "" {
		config = []byte("[]\n")
	}
	return agentPreparation{Commit: func() error {
		if err := secureWrite(path, config); err != nil {
			return err
		}
		return secureWrite(homeConfig, cleaned)
	}}, nil
}

func (dshDriver) Administrative(args []string) bool {
	return (defaultAgentBehavior{}).Administrative(args) || shared.Contains(args, "--dump-default-config") || len(args) > 0 && args[0] == "plugin"
}

func (dshDriver) Launch(root string, args []string, administrative bool) ([]string, map[string]string, error) {
	if administrative || shared.Contains(args, "--dump-default-config") || len(args) > 0 && args[0] == "plugin" {
		return args, nil, nil
	}
	path := dshScopedPath(root)
	if _, err := os.Stat(path); err != nil {
		return nil, nil, err
	}
	// The native parser expands the profile shorthand only in argv[0] and stops
	// parsing launcher options at the first app argument. Normalize before prepend.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append([]string{"--profile"}, args...)
	}
	return append([]string{"--patch", path}, args...), nil, nil
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
