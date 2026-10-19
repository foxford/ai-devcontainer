package runtime

import (
	"encoding/json"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// The runtime owns orchestration; adapters own native formats, CLI arguments,
// installation and context layout. Prepare is read-only. Commit runs only after
// every adapter has prepared and retry state is persisted under the delivery lock.
type agentDriver interface {
	Name() string
	Context() agentContextLayout
	Install(*toolInstaller) error
	ProjectSkills(string, map[string]bool) error
	Prepare(agentDelivery) (agentPreparation, error)
	Administrative([]string) bool
	Launch(string, []string, bool) ([]string, map[string]string, error)
}

type agentState struct {
	Servers []string
	Data    json.RawMessage
	Legacy  json.RawMessage
}

type agentCheckout struct {
	Namespace string
	State     agentState
}

type agentDelivery struct {
	Project         shared.Project
	Home, Namespace string
	Servers         map[string]MCPServer
	OAuth           map[string]bool
	Previous        agentState
	Registry        []agentCheckout
}

type agentPreparation struct {
	Commit       func() error
	PendingState json.RawMessage
	FinalState   json.RawMessage
}

// Composition root: adding/replacing an adapter does not change orchestration.
func agentDrivers() []agentDriver {
	return []agentDriver{claudeDriver{}, codexDriver{}, openCodeDriver{}, hermesDriver{}, dshDriver{}}
}

func agentNames() []string {
	names := []string{}
	for _, driver := range agentDrivers() {
		names = append(names, driver.Name())
	}
	return names
}

func jsonAgentFile(path string, value any) (agentPreparation, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return agentPreparation{}, err
	}
	return agentPreparation{Commit: func() error { return secureWrite(path, append(data, '\n')) }}, nil
}

// These CLI conventions are shared by the bundled adapters. An adapter can
// override either method when its native command line uses different rules.
type defaultAgentBehavior struct{}

func (defaultAgentBehavior) Administrative(args []string) bool {
	for _, arg := range args {
		if arg == "--version" || arg == "--help" || arg == "-h" {
			return true
		}
	}
	return len(args) > 0 && shared.Contains([]string{"mcp", "login", "logout", "auth", "skills", "setup", "config"}, args[0])
}
func (defaultAgentBehavior) Launch(_ string, args []string, _ bool) ([]string, map[string]string, error) {
	return args, nil, nil
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
