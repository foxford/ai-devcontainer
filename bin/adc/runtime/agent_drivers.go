package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// Every driver prepares its native configuration before any agent file changes.
// The returned operation commits under the shared delivery lock. Adding an agent
// does not change capability activation or MCP transport discovery.
type agentDriver interface {
	Name() string
	Prepare(agentDelivery) (func() error, error)
	Launch(string, []string, bool) ([]string, map[string]string, error)
}
type agentDelivery struct {
	Project         shared.Project
	Home, Namespace string
	Servers         map[string]MCPServer
	OAuth           map[string]bool
	Previous        mcpState
	Headers         []string
}

func agentDrivers() []agentDriver {
	return []agentDriver{claudeDriver{}, codexDriver{}, openCodeDriver{}, hermesDriver{}, dshDriver{}}
}

func jsonAgentFile(path string, value any) (func() error, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return func() error { return secureWrite(path, append(data, '\n')) }, nil
}

type claudeDriver struct{ plainAgentLaunch }

func (claudeDriver) Name() string { return "claude" }
func (claudeDriver) Prepare(d agentDelivery) (func() error, error) {
	path := filepath.Join(d.Project.RuntimePath, ".mcp.json")
	if !d.Previous.ClaudeOwned {
		// Supply an additional CLI config instead of changing the human file.
		return jsonAgentFile(filepath.Join(d.Project.RuntimePath, shared.ProjectStateDir, "claude-mcp.json"), map[string]any{"mcpServers": d.Servers})
	}
	if len(d.Servers) == 0 {
		return func() error {
			err := os.Remove(path)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}, nil
	}
	return jsonAgentFile(path, map[string]any{"mcpServers": d.Servers})
}

type codexDriver struct{ plainAgentLaunch }

func (codexDriver) Name() string { return "codex" }
func (codexDriver) Prepare(d agentDelivery) (func() error, error) {
	return func() error { return syncCodex(d.Servers, d.Previous.Servers, d.OAuth, d.Headers) }, nil
}

type openCodeDriver struct{ plainAgentLaunch }

func (openCodeDriver) Name() string { return "opencode" }
func (openCodeDriver) Prepare(d agentDelivery) (func() error, error) {
	servers := map[string]any{}
	for name, server := range d.Servers {
		converted := map[string]any{"enabled": true}
		if address, _ := server["url"].(string); address != "" {
			converted["type"] = "remote"
			converted["url"] = address
			if headers, ok := server["headers"].(map[string]any); ok {
				converted["headers"] = headers
			}
			if !d.OAuth[name] {
				converted["oauth"] = false
			}
		} else {
			command, _ := server["command"].(string)
			if command == "" {
				return nil, fmt.Errorf("MCP %s: нет команды", name)
			}
			argv := []any{command}
			if args, ok := server["args"].([]any); ok {
				argv = append(argv, args...)
			}
			converted["type"] = "local"
			converted["command"] = argv
			converted["cwd"] = d.Project.RuntimePath
			if cwd, ok := server["cwd"].(string); ok {
				converted["cwd"] = cwd
			}
			if env, ok := server["env"].(map[string]any); ok {
				converted["environment"] = env
			}
		}
		servers[name] = converted
	}
	return jsonAgentFile(filepath.Join(d.Project.RuntimePath, shared.ProjectStateDir, "opencode.json"), map[string]any{
		"mcp":          servers,
		"instructions": []string{filepath.Join(d.Project.RuntimePath, "AGENTS.platform.md"), filepath.Join(d.Project.RuntimePath, "AGENTS.skills.md"), "/workspace/AGENTS.workspace.md"},
	})
}

// OpenCode merges this final inline layer with its own global/project configs.
// An existing user inline layer wins conflicting keys and keeps its preferences.
func openCodeEnvironment(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, shared.ProjectStateDir, "opencode.json"))
	if err != nil {
		return "", err
	}
	var generated map[string]any
	if err := json.Unmarshal(data, &generated); err != nil {
		return "", err
	}
	if content := os.Getenv("OPENCODE_CONFIG_CONTENT"); content != "" {
		var user map[string]any
		if json.Unmarshal([]byte(content), &user) != nil || user == nil {
			return "", errors.New("OPENCODE_CONFIG_CONTENT должен быть JSON объектом")
		}
		generated = mergeObject(generated, user)
	}
	data, err = json.Marshal(generated)
	return string(data), err
}

type hermesDriver struct{ plainAgentLaunch }

func (hermesDriver) Name() string { return "hermes" }
func (hermesDriver) Prepare(d agentDelivery) (func() error, error) {
	path := filepath.Join(d.Home, ".hermes", "config.yaml")
	data, err := hermesMCP(path, d.Servers, d.Previous.Servers)
	if err != nil {
		return nil, err
	}
	return func() error { return secureWrite(path, data) }, nil
}

type dshDriver struct{ plainAgentLaunch }

func (dshDriver) Name() string { return "dsh" }
func (dshDriver) Prepare(d agentDelivery) (func() error, error) {
	path := filepath.Join(d.Home, ".dsh", "cordis.patch.yml")
	data, err := dshMCP(path, d.Namespace, d.Project.RuntimePath, d.Servers, d.OAuth)
	if err != nil {
		return nil, err
	}
	return func() error { return secureWrite(path, data) }, nil
}

// Launch configuration is part of the same driver, including administrative CLI
// calls. The adapter restores environment overrides after the child exits.
type plainAgentLaunch struct{}

func (plainAgentLaunch) Launch(_ string, args []string, _ bool) ([]string, map[string]string, error) {
	return args, nil, nil
}
func (claudeDriver) Launch(root string, args []string, administrative bool) ([]string, map[string]string, error) {
	if administrative {
		return args, nil, nil
	}
	state, exists, err := readMCPState(root)
	if err != nil {
		return nil, nil, err
	}
	if exists && !state.ClaudeOwned {
		args = append(args, "--mcp-config", filepath.Join(root, shared.ProjectStateDir, "claude-mcp.json"))
	}
	return args, nil, nil
}
func (openCodeDriver) Launch(root string, args []string, administrative bool) ([]string, map[string]string, error) {
	content, err := openCodeEnvironment(root)
	if os.IsNotExist(err) && administrative {
		return args, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return args, map[string]string{"OPENCODE_CONFIG_CONTENT": content}, nil
}
func findAgentDriver(name string) agentDriver {
	for _, driver := range agentDrivers() {
		if driver.Name() == name {
			return driver
		}
	}
	return nil
}

// IsAgent is shared by named CLI adapters and explicit adc agent launches.
func IsAgent(name string) bool { return findAgentDriver(name) != nil }
