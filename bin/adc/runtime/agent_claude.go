package runtime

import (
	"encoding/json"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
)

type claudeDriver struct{ defaultAgentBehavior }

func (claudeDriver) Name() string { return "claude" }

type claudeState struct {
	Owned bool `json:"owned"`
}

func decodeClaudeState(previous agentState) (claudeState, error) {
	var state claudeState
	if len(previous.Data) > 0 {
		err := json.Unmarshal(previous.Data, &state)
		return state, err
	}
	if len(previous.Legacy) > 0 {
		var legacy struct {
			Version int  `json:"version"`
			Owned   bool `json:"claudeOwned"`
		}
		if err := json.Unmarshal(previous.Legacy, &legacy); err != nil {
			return state, err
		}
		state.Owned = legacy.Version == 0 || legacy.Owned
	}
	return state, nil
}

func (claudeDriver) Prepare(d agentDelivery) (agentPreparation, error) {
	path := filepath.Join(d.Project.RuntimePath, ".mcp.json")
	state, err := decodeClaudeState(d.Previous)
	if err != nil {
		return agentPreparation{}, err
	}
	if len(d.Previous.Data) == 0 && len(d.Previous.Legacy) == 0 {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			state.Owned = true
		} else if err != nil {
			return agentPreparation{}, err
		}
	}
	prepare := func() (agentPreparation, error) {
		if !state.Owned {
			// Supply an additional CLI config instead of changing the human file.
			return jsonAgentFile(filepath.Join(d.Project.RuntimePath, shared.ProjectStateDir, "claude-mcp.json"), map[string]any{"mcpServers": d.Servers})
		}
		if len(d.Servers) == 0 {
			return agentPreparation{Commit: func() error {
				err := os.Remove(path)
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}}, nil
		}
		return jsonAgentFile(path, map[string]any{"mcpServers": d.Servers})
	}
	result, err := prepare()
	if err != nil {
		return result, err
	}
	data, err := json.Marshal(state)
	result.PendingState, result.FinalState = data, data
	return result, err
}

func (claudeDriver) Launch(root string, args []string, administrative bool) ([]string, map[string]string, error) {
	if administrative {
		return args, nil, nil
	}
	state, exists, err := readMCPState(root)
	if err != nil {
		return nil, nil, err
	}
	private, err := decodeClaudeState(state.forDriver("claude"))
	if err != nil {
		return nil, nil, err
	}
	if exists && !private.Owned {
		args = append(args, "--mcp-config", filepath.Join(root, shared.ProjectStateDir, "claude-mcp.json"))
	}
	return args, nil, nil
}
