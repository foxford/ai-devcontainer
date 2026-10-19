package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
)

type openCodeDriver struct{ defaultAgentBehavior }

func (openCodeDriver) Name() string { return "opencode" }
func (openCodeDriver) Prepare(d agentDelivery) (agentPreparation, error) {
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
				return agentPreparation{}, fmt.Errorf("MCP %s: нет команды", name)
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
