package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

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

func (a *App) DeliverMCP(p shared.Project) error {
	return a.deliverMCP(p, agentDrivers())
}

func (a *App) deliverMCP(p shared.Project, drivers []agentDriver) error {
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
	state, _, err := readMCPState(p.RuntimePath)
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
	registry, err := a.mcpRegistry(p, state)
	if err != nil {
		return err
	}
	return a.applyAgentDelivery(agentDelivery{Project: p, Home: home, Namespace: namespace, Servers: servers, OAuth: oauth}, state, registry, drivers)
}

func copyDriverStates(states map[string]agentRecord) map[string]agentRecord {
	result := make(map[string]agentRecord, len(states))
	for name, state := range states {
		result[name] = agentRecord{Servers: append([]string{}, state.Servers...), State: append(json.RawMessage(nil), state.State...)}
	}
	return result
}

// All adapters prepare before any writes. Pending ownership survives partial
// commits; final ownership is published only after every adapter succeeds.
func (a *App) applyAgentDelivery(delivery agentDelivery, state mcpState, registry []checkoutMCPState, drivers []agentDriver) error {
	pending, final := state, state
	pending.Version, final.Version = 2, 2
	pending.Drivers, final.Drivers = copyDriverStates(state.Drivers), copyDriverStates(state.Drivers)
	names := map[string]bool{}
	for _, name := range state.Servers {
		names[name] = true
	}
	for name := range delivery.Servers {
		names[name] = true
	}
	pending.Servers, final.Servers = sortedKeys(names), sortedKeys(delivery.Servers)
	preparations := make([]agentPreparation, 0, len(drivers))
	seen := map[string]bool{}
	for _, driver := range drivers {
		name := driver.Name()
		if name == "" || seen[name] {
			return fmt.Errorf("некорректное или повторное имя agent driver: %q", name)
		}
		seen[name] = true
		input := delivery
		// Native conversion must not mutate the plan seen by another adapter.
		input.Servers = make(map[string]MCPServer, len(delivery.Servers))
		for server, config := range delivery.Servers {
			input.Servers[server] = MCPServer(mergeObject(nil, map[string]any(config)))
		}
		input.OAuth = make(map[string]bool, len(delivery.OAuth))
		for server, enabled := range delivery.OAuth {
			input.OAuth[server] = enabled
		}
		input.Previous = state.forDriver(name)
		input.Registry = make([]agentCheckout, 0, len(registry))
		for _, checkout := range registry {
			input.Registry = append(input.Registry, agentCheckout{Namespace: checkout.Namespace, State: checkout.State.forDriver(name)})
		}
		prepared, err := driver.Prepare(input)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if prepared.Commit == nil {
			return fmt.Errorf("%s: отсутствует операция применения", name)
		}
		for _, data := range []json.RawMessage{prepared.PendingState, prepared.FinalState} {
			if len(data) > 0 && !json.Valid(data) {
				return fmt.Errorf("%s: некорректное состояние драйвера", name)
			}
		}
		previous := state.forDriver(name)
		owned := map[string]bool{}
		for _, server := range previous.Servers {
			owned[server] = true
		}
		for server := range delivery.Servers {
			owned[server] = true
		}
		pendingRecord := agentRecord{Servers: sortedKeys(owned), State: previous.Data}
		finalRecord := agentRecord{Servers: sortedKeys(delivery.Servers), State: previous.Data}
		// A stateless adapter does not erase state belonging to an earlier version.
		if len(prepared.PendingState) > 0 {
			pendingRecord.State = append(json.RawMessage(nil), prepared.PendingState...)
		}
		if len(prepared.FinalState) > 0 {
			finalRecord.State = append(json.RawMessage(nil), prepared.FinalState...)
		}
		pending.Drivers[name], final.Drivers[name] = pendingRecord, finalRecord
		preparations = append(preparations, prepared)
	}
	statePath := filepath.Join(delivery.Project.RuntimePath, shared.ProjectStateDir, "mcp-state.json")
	write := func(state mcpState) error {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		return secureWrite(statePath, append(data, '\n'))
	}
	if err := write(pending); err != nil {
		return err
	}
	if err := a.ensureProjectSecrets(delivery.Project.RuntimePath); err != nil {
		return err
	}
	for index, prepared := range preparations {
		if err := prepared.Commit(); err != nil {
			return fmt.Errorf("%s: %w", drivers[index].Name(), err)
		}
	}
	return write(final)
}
