package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// This adapter has no native agent dependency. The same contract must suffice
// when every bundled adapter is replaced by a different implementation.
type contractAgentDriver struct {
	name    string
	prepare func(agentDelivery) (agentPreparation, error)
}

func (d contractAgentDriver) Name() string                              { return d.name }
func (contractAgentDriver) Context() agentContextLayout                 { return agentContextLayout{} }
func (contractAgentDriver) Install(*toolInstaller) error                { return nil }
func (contractAgentDriver) ProjectSkills(string, map[string]bool) error { return nil }
func (d contractAgentDriver) Prepare(input agentDelivery) (agentPreparation, error) {
	return d.prepare(input)
}
func (contractAgentDriver) Administrative([]string) bool { return false }
func (contractAgentDriver) Launch(_ string, args []string, _ bool) ([]string, map[string]string, error) {
	return args, nil, nil
}

func assertContractJSON(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("invalid actual driver state %q: %v", got, err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("invalid expected driver state %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("driver state = %s; want %s", got, want)
	}
}

func TestAgentContractDeliveryPreservesOpaqueStateAcrossRetry(t *testing.T) {
	a, project, store := mcpFixture(t)
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner"}}}`)
	namespace := project.ID + "-" + shared.CheckoutID(project.RuntimePath)
	retired, current, absentOwned := namespace+"-retired", namespace+"-service", namespace+"-absent-owned"
	initialPayload := json.RawMessage(`{"revision":"old","resources":["previous-resource"]}`)
	pendingPayload := json.RawMessage(`{"revision":"pending","resources":["previous-resource","replacement-resource"]}`)
	finalPayload := json.RawMessage(`{"revision":"final","resources":["replacement-resource"]}`)
	absentPayload := json.RawMessage(`{"vendorSpecific":{"list":[1,true,null],"version":"future"}}`)
	legacy := json.RawMessage(`{"version":1,"servers":["old"],"unknownAdapterField":{"keep":true}}`)
	initial := mcpState{
		Version: 2,
		Servers: []string{retired},
		Drivers: map[string]agentRecord{
			"replacement":    {Servers: []string{retired}, State: initialPayload},
			"absent-adapter": {Servers: []string{absentOwned}, State: absentPayload},
		},
		Legacy: legacy,
	}
	initialBytes, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(project.RuntimePath, shared.ProjectStateDir, "mcp-state.json")
	writeFixture(t, statePath, string(initialBytes))
	secretsPath := filepath.Join(project.RuntimePath, ".agents", "mcp.secrets.env")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(home, ".codex", "config.toml")
	malformedNativeConfig := "mcp_servers = { broken native configuration\n"
	writeFixture(t, codexPath, malformedNativeConfig)
	commitFailure := errors.New("replacement adapter failed after partial write")
	attempt := 0
	var events []string
	assertState := func(state mcpState, names []string, payload json.RawMessage) {
		t.Helper()
		if state.Version != 2 || !reflect.DeepEqual(state.Servers, names) {
			t.Fatalf("delivery state = %#v; want version 2 with servers %v", state, names)
		}
		if !reflect.DeepEqual(state.Drivers["replacement"].Servers, names) {
			t.Fatalf("adapter ownership = %v; want %v", state.Drivers["replacement"].Servers, names)
		}
		if !reflect.DeepEqual(state.Drivers["absent-adapter"].Servers, []string{absentOwned}) {
			t.Fatal("delivery changed ownership of an absent adapter")
		}
		assertContractJSON(t, state.Drivers["replacement"].State, payload)
		assertContractJSON(t, state.Drivers["absent-adapter"].State, absentPayload)
		assertContractJSON(t, state.Legacy, legacy)
	}
	readState := func() mcpState {
		t.Helper()
		state, exists, err := inspectMCPState(project.RuntimePath)
		if err != nil || !exists {
			t.Fatalf("read state: exists=%v err=%v", exists, err)
		}
		return state
	}
	driver := contractAgentDriver{name: "replacement", prepare: func(input agentDelivery) (agentPreparation, error) {
		attempt++
		events = append(events, "prepare")
		if input.Project.RuntimePath != project.RuntimePath || input.Home != home || input.Namespace != namespace {
			t.Fatalf("adapter received wrong checkout: %#v", input)
		}
		if len(input.Servers) != 1 || input.Servers[current]["command"] != "runner" {
			t.Fatalf("adapter received wrong MCP plan: %#v", input.Servers)
		}
		previousNames, previousPayload := []string{retired}, initialPayload
		if attempt == 1 {
			if got := readFixture(t, statePath); got != string(initialBytes) {
				t.Fatal("delivery wrote state before preparation")
			}
			if _, err := os.Stat(secretsPath); !os.IsNotExist(err) {
				t.Fatalf("delivery wrote secrets before preparation: %v", err)
			}
		} else {
			previousNames, previousPayload = []string{retired, current}, pendingPayload
		}
		if !reflect.DeepEqual(input.Previous.Servers, previousNames) {
			t.Fatalf("previous servers = %v; want %v", input.Previous.Servers, previousNames)
		}
		assertContractJSON(t, input.Previous.Data, previousPayload)
		assertContractJSON(t, input.Previous.Legacy, legacy)
		if len(input.Registry) != 1 || input.Registry[0].Namespace != namespace {
			t.Fatalf("wrong checkout registry: %#v", input.Registry)
		}
		assertContractJSON(t, input.Registry[0].State.Data, previousPayload)
		return agentPreparation{
			PendingState: pendingPayload,
			FinalState:   finalPayload,
			Commit: func() error {
				events = append(events, "commit")
				assertState(readState(), []string{retired, current}, pendingPayload)
				if attempt == 1 {
					return commitFailure
				}
				return nil
			},
		}, nil
	}}
	if err := a.deliverMCP(project, []agentDriver{driver}); !errors.Is(err, commitFailure) {
		t.Fatalf("delivery failure = %v; want adapter error", err)
	}
	assertState(readState(), []string{retired, current}, pendingPayload)
	if err := a.deliverMCP(project, []agentDriver{driver}); err != nil {
		t.Fatal(err)
	}
	assertState(readState(), []string{current}, finalPayload)
	if !reflect.DeepEqual(events, []string{"prepare", "commit", "prepare", "commit"}) {
		t.Fatalf("unexpected adapter lifecycle: %v", events)
	}
	if readFixture(t, codexPath) != malformedNativeConfig {
		t.Fatal("delivery touched a native configuration without its adapter")
	}
	// Removing an adapter must retain enough ownership to clean up its old
	// servers if it returns after other adapters have completed deliveries.
	returned := false
	returningDriver := contractAgentDriver{name: "absent-adapter", prepare: func(input agentDelivery) (agentPreparation, error) {
		returned = true
		if !reflect.DeepEqual(input.Previous.Servers, []string{absentOwned}) {
			t.Fatalf("returning adapter lost stale ownership: %v", input.Previous.Servers)
		}
		assertContractJSON(t, input.Previous.Data, absentPayload)
		return agentPreparation{Commit: func() error { return nil }}, nil
	}}
	if err := a.deliverMCP(project, []agentDriver{returningDriver}); err != nil {
		t.Fatal(err)
	}
	state := readState()
	if !returned || !reflect.DeepEqual(state.Drivers["absent-adapter"].Servers, []string{current}) {
		t.Fatal("returning adapter did not publish current ownership")
	}
	assertContractJSON(t, state.Drivers["replacement"].State, finalPayload)
	assertContractJSON(t, state.Drivers["absent-adapter"].State, absentPayload)
	assertContractJSON(t, state.Legacy, legacy)
}

func TestAgentContractPreparationFailureLeavesDeliveryUntouched(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "first-delivery"
		if existing {
			name = "existing-state"
		}
		t.Run(name, func(t *testing.T) {
			a, project, _ := mcpFixture(t)
			statePath := filepath.Join(project.RuntimePath, shared.ProjectStateDir, "mcp-state.json")
			before := `{"version":2,"servers":["existing"],"drivers":{"absent":{"servers":["existing"],"state":{"owned":true}}}}`
			if existing {
				writeFixture(t, statePath, before)
			}
			preflightFailure := errors.New("invalid native config")
			var events []string
			first := contractAgentDriver{name: "first", prepare: func(agentDelivery) (agentPreparation, error) {
				events = append(events, "first prepare")
				return agentPreparation{PendingState: json.RawMessage(`{"changed":true}`), Commit: func() error {
					events = append(events, "first commit")
					return nil
				}}, nil
			}}
			second := contractAgentDriver{name: "second", prepare: func(agentDelivery) (agentPreparation, error) {
				events = append(events, "second prepare")
				return agentPreparation{}, preflightFailure
			}}
			if err := a.deliverMCP(project, []agentDriver{first, second}); !errors.Is(err, preflightFailure) {
				t.Fatalf("delivery failure = %v; want preflight error", err)
			}
			if !reflect.DeepEqual(events, []string{"first prepare", "second prepare"}) {
				t.Fatalf("commit happened before all adapters prepared: %v", events)
			}
			if existing {
				if readFixture(t, statePath) != before {
					t.Fatal("preparation failure changed existing checkpoint")
				}
			} else if _, err := os.Stat(statePath); !os.IsNotExist(err) {
				t.Fatalf("preparation failure created a checkpoint: %v", err)
			}
			if _, err := os.Stat(filepath.Join(project.RuntimePath, ".agents", "mcp.secrets.env")); !os.IsNotExist(err) {
				t.Fatalf("preparation failure wrote project secrets: %v", err)
			}
		})
	}
}

func TestAgentContractLegacyStateInspectionIsReadOnly(t *testing.T) {
	for _, fixture := range []struct {
		name, file, data string
		legacy           json.RawMessage
	}{
		{
			name:   "json",
			file:   "mcp-state.json",
			data:   `{"version":1,"servers":["first","second"],"unknownDriverField":{"mustSurvive":true}}`,
			legacy: json.RawMessage(`{"version":1,"servers":["first","second"],"unknownDriverField":{"mustSurvive":true}}`),
		},
		{
			name:   "text",
			file:   "mcp-state",
			data:   "first\nsecond\n",
			legacy: json.RawMessage(`{"version":0}`),
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".ai-devcontainer", fixture.file)
			writeFixture(t, path, fixture.data)
			state, exists, err := inspectMCPState(root)
			if err != nil || !exists {
				t.Fatalf("inspect legacy state: exists=%v err=%v", exists, err)
			}
			if state.Version != 2 || !reflect.DeepEqual(state.Servers, []string{"first", "second"}) {
				t.Fatalf("wrong converted state: %#v", state)
			}
			assertContractJSON(t, state.forDriver("replacement").Legacy, fixture.legacy)
			if readFixture(t, path) != fixture.data {
				t.Fatal("registry inspection changed legacy state")
			}
			if _, err := os.Stat(filepath.Join(root, shared.ProjectStateDir)); !os.IsNotExist(err) {
				t.Fatalf("registry inspection migrated neighbouring checkout: %v", err)
			}
		})
	}
}

func TestAgentContractAbsentDriverRetainsLegacyOwnership(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			a, project, store := mcpFixture(t)
			writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner"}}}`)
			namespace := project.ID + "-" + shared.CheckoutID(project.RuntimePath)
			retired, current := namespace+"-retired", namespace+"-service"
			filename, original := "mcp-state", retired+"\n"
			if format == "json" {
				filename = "mcp-state.json"
				data, err := json.Marshal(map[string]any{
					"version": 1, "servers": []string{retired}, "absentDriverField": "keep",
				})
				if err != nil {
					t.Fatal(err)
				}
				original = string(data)
			}
			writeFixture(t, filepath.Join(project.RuntimePath, shared.ProjectStateDir, filename), original)
			replacement := contractAgentDriver{name: "replacement", prepare: func(agentDelivery) (agentPreparation, error) {
				return agentPreparation{Commit: func() error { return nil }}, nil
			}}
			if err := a.deliverMCP(project, []agentDriver{replacement}); err != nil {
				t.Fatal(err)
			}
			state, _, err := inspectMCPState(project.RuntimePath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(state.Servers, []string{current}) {
				t.Fatalf("replacement delivery did not finalize its names: %v", state.Servers)
			}
			returning := contractAgentDriver{name: "absent", prepare: func(input agentDelivery) (agentPreparation, error) {
				want := []string{retired, current}
				if !reflect.DeepEqual(input.Previous.Servers, want) {
					t.Fatalf("absent adapter lost legacy ownership after migration: got %v, want %v", input.Previous.Servers, want)
				}
				if len(input.Registry) != 1 || !reflect.DeepEqual(input.Registry[0].State.Servers, want) {
					t.Fatalf("registry lost legacy ownership: %#v", input.Registry)
				}
				if format == "json" {
					assertContractJSON(t, input.Previous.Legacy, json.RawMessage(original))
				}
				return agentPreparation{Commit: func() error { return nil }}, nil
			}}
			if err := a.deliverMCP(project, []agentDriver{returning}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgentContractNativeConversionCannotChangeSiblingOrOwnership(t *testing.T) {
	a, project, store := mcpFixture(t)
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner","args":["original"],"env":{"VALUE":"original"},"x-oauth":true}}}`)
	namespace := project.ID + "-" + shared.CheckoutID(project.RuntimePath)
	current, oldFirst, oldSecond := namespace+"-service", namespace+"-first-old", namespace+"-second-old"
	firstPayload, secondPayload := json.RawMessage(`{"value":"A"}`), json.RawMessage(`{"value":"B"}`)
	legacy := json.RawMessage(`{"version":1,"sentinel":"original"}`)
	initial := mcpState{
		Version: 2, Servers: []string{oldFirst, oldSecond}, Legacy: legacy,
		Drivers: map[string]agentRecord{
			"first":  {Servers: []string{oldFirst}, State: firstPayload},
			"second": {Servers: []string{oldSecond}, State: secondPayload},
		},
	}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(project.RuntimePath, shared.ProjectStateDir, "mcp-state.json"), string(data))
	assertOwnership := func(pending bool) {
		t.Helper()
		state, _, err := inspectMCPState(project.RuntimePath)
		if err != nil {
			t.Fatal(err)
		}
		for name, previous := range map[string]string{"first": oldFirst, "second": oldSecond} {
			want := []string{current}
			if pending {
				want = []string{previous, current}
			}
			if !reflect.DeepEqual(state.Drivers[name].Servers, want) {
				t.Fatalf("%s ownership used adapter-mutated input: %v; want %v", name, state.Drivers[name].Servers, want)
			}
		}
		assertContractJSON(t, state.Drivers["first"].State, firstPayload)
		assertContractJSON(t, state.Drivers["second"].State, secondPayload)
		assertContractJSON(t, state.Legacy, legacy)
	}
	first := contractAgentDriver{name: "first", prepare: func(input agentDelivery) (agentPreparation, error) {
		// An adapter may normalize its local copy into a native format. That
		// must affect neither another adapter nor the persisted ownership.
		input.Servers[current]["env"].(map[string]any)["VALUE"] = "native"
		input.Servers[current]["args"].([]any)[0] = "native"
		input.Servers["invented"] = MCPServer{"command": "native"}
		input.OAuth[current] = false
		input.Previous.Servers[0] = "invented"
		copy(input.Previous.Data, []byte(`{"value":"X"}`))
		copy(input.Previous.Legacy, []byte(`{"version":1,"sentinel":"modified"}`))
		input.Registry[0].State.Servers[0] = "invented"
		copy(input.Registry[0].State.Data, []byte(`{"value":"X"}`))
		copy(input.Registry[0].State.Legacy, []byte(`{"version":1,"sentinel":"modified"}`))
		return agentPreparation{Commit: func() error {
			assertOwnership(true)
			return nil
		}}, nil
	}}
	second := contractAgentDriver{name: "second", prepare: func(input agentDelivery) (agentPreparation, error) {
		if len(input.Servers) != 1 || input.Servers[current]["env"].(map[string]any)["VALUE"] != "original" || !reflect.DeepEqual(input.Servers[current]["args"], []any{"original"}) || !input.OAuth[current] {
			t.Fatalf("native conversion changed the sibling MCP plan: %#v; OAuth: %v", input.Servers, input.OAuth)
		}
		if !reflect.DeepEqual(input.Previous.Servers, []string{oldSecond}) || !reflect.DeepEqual(input.Registry[0].State.Servers, []string{oldSecond}) {
			t.Fatal("native conversion changed the sibling ownership")
		}
		assertContractJSON(t, input.Previous.Data, secondPayload)
		assertContractJSON(t, input.Registry[0].State.Data, secondPayload)
		assertContractJSON(t, input.Previous.Legacy, legacy)
		assertContractJSON(t, input.Registry[0].State.Legacy, legacy)
		return agentPreparation{Commit: func() error { return nil }}, nil
	}}
	if err := a.deliverMCP(project, []agentDriver{first, second}); err != nil {
		t.Fatal(err)
	}
	assertOwnership(false)
}
