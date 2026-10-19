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

// Driver payloads are opaque to delivery. Keep legacy data until each adapter
// has migrated its own state, including adapters absent from this installation.
type mcpState struct {
	Version       int                    `json:"version"`
	Servers       []string               `json:"servers"`
	Drivers       map[string]agentRecord `json:"drivers,omitempty"`
	Legacy        json.RawMessage        `json:"legacy,omitempty"`
	LegacyServers []string               `json:"legacyServers,omitempty"`
}

type agentRecord struct {
	Servers []string        `json:"servers"`
	State   json.RawMessage `json:"state,omitempty"`
}

func (s mcpState) forDriver(name string) agentState {
	names, data := s.Servers, json.RawMessage(nil)
	if record, ok := s.Drivers[name]; ok {
		names, data = record.Servers, record.State
	} else if len(s.LegacyServers) > 0 {
		owned := map[string]bool{}
		for _, name := range s.Servers {
			owned[name] = true
		}
		for _, name := range s.LegacyServers {
			owned[name] = true
		}
		names = sortedKeys(owned)
	}
	return agentState{
		Servers: append([]string{}, names...),
		Data:    append(json.RawMessage(nil), data...),
		Legacy:  append(json.RawMessage(nil), s.Legacy...),
	}
}

func decodeMCPState(data []byte) (mcpState, error) {
	var state mcpState
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	switch state.Version {
	case 1:
		state.Legacy = append(json.RawMessage(nil), data...)
		state.LegacyServers = append([]string{}, state.Servers...)
		state.Version = 2
	case 2:
	default:
		return state, fmt.Errorf("неподдерживаемая версия MCP state: %d", state.Version)
	}
	return state, nil
}

func readMCPState(root string) (mcpState, bool, error) {
	if err := shared.MigrateProjectState(root); err != nil {
		return mcpState{}, false, err
	}
	return inspectMCPState(root)
}

// Registry inspection must never migrate or write a neighbour's files.
func inspectMCPState(root string) (mcpState, bool, error) {
	for _, directory := range []string{shared.ProjectStateDir, ".ai-devcontainer"} {
		path := filepath.Join(root, directory, "mcp-state.json")
		data, err := os.ReadFile(path)
		if err == nil {
			state, err := decodeMCPState(data)
			if err != nil {
				return state, false, fmt.Errorf("%s: %w", path, err)
			}
			return state, true, nil
		}
		if !os.IsNotExist(err) {
			return mcpState{}, false, err
		}
		data, err = os.ReadFile(filepath.Join(root, directory, "mcp-state"))
		if err == nil {
			names := strings.Fields(string(data))
			return mcpState{Version: 2, Servers: names, LegacyServers: append([]string{}, names...), Legacy: json.RawMessage(`{"version":0}`)}, true, nil
		}
		if !os.IsNotExist(err) {
			return mcpState{}, false, err
		}
	}
	return mcpState{Version: 2, Servers: []string{}}, false, nil
}

type checkoutMCPState struct {
	Namespace string
	State     mcpState
}

func (a *App) mcpRegistry(p shared.Project, current mcpState) ([]checkoutMCPState, error) {
	result := []checkoutMCPState{{Namespace: p.ID + "-" + shared.CheckoutID(p.RuntimePath), State: current}}
	workspace, err := a.shared().Load()
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, checkout := range allSyncCheckouts(workspace.Projects) {
		if filepath.Clean(checkout.RuntimePath) == filepath.Clean(p.RuntimePath) {
			continue
		}
		state, _, err := inspectMCPState(checkout.RuntimePath)
		if err != nil {
			return nil, err
		}
		result = append(result, checkoutMCPState{Namespace: checkout.ID + "-" + shared.CheckoutID(checkout.RuntimePath), State: state})
	}
	return result, nil
}
