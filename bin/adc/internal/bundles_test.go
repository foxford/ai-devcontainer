package shared

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func TestBundleExportsResolveFromManifest(t *testing.T) {
	a := &App{Platform: testutil.Delivery(t)}
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"graphify", "obsidian"} {
		capability := catalog.Capabilities[name]
		prefix := "bundles/" + name + "/"
		if capability.Bundle != prefix+"bundle.json" || len(capability.Instructions) == 0 || len(capability.Skills) != 1 || capability.MCPSource != prefix+"mcp.json" {
			t.Fatal("incomplete exports", capability)
		}
		if !strings.HasPrefix(capability.Skills[0], prefix) || catalog.MCPSources[name] != capability.MCPSource {
			t.Fatal("bundle source not preserved")
		}
		if len(capability.MCP) != 1 || capability.MCP[0] != name {
			t.Fatal("MCP export missing")
		}
	}
	if len(catalog.Capabilities["graphify"].Tools) != 1 || len(catalog.Capabilities["obsidian"].Tools) != 0 {
		t.Fatal("installation confused with MCP delivery")
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var registry struct {
		Capabilities map[string]map[string]json.RawMessage
	}
	if err = json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.Capabilities["graphify"]) != 1 {
		t.Fatal("resolved payload duplicated in registry")
	}
	root := t.TempDir()
	caps, err := a.ProjectCapabilities(Project{RuntimePath: root, Capabilities: []string{"graphify"}})
	if err != nil {
		t.Fatal(err)
	}
	env, err := a.DeliveryEnv(Project{RuntimePath: root, Capabilities: caps})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(env, "\n"), `ADC_PLATFORM_MCP_SERVERS=["graphify"]`) {
		t.Fatal("bundle MCP not selected")
	}
}
func TestBundleRejectsBrokenAndAmbiguousExports(t *testing.T) {
	for _, mode := range []string{"escape", "wrong-name", "missing-skill", "missing-mcp", "unknown-export", "duplicate-server", "non-object-server", "activation-server", "activation-key", "activation-secret-default"} {
		t.Run(mode, func(t *testing.T) {
			a := &App{Platform: testutil.Delivery(t)}
			path := a.DeliveryPath("bundles/graphify/bundle.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var bundle map[string]any
			if err = json.Unmarshal(data, &bundle); err != nil {
				t.Fatal(err)
			}
			exports := bundle["exports"].(map[string]any)
			switch mode {
			case "activation-server", "activation-key", "activation-secret-default":
				prompt := map[string]any{"key": "TOKEN", "label": "API key", "secret": true}
				setup := map[string]any{"server": "graphify", "requirements": []string{"fixture"}, "prompts": []any{prompt}}
				if mode == "activation-server" {
					setup["server"] = "absent"
				}
				if mode == "activation-key" {
					prompt["key"] = "BAD-ENV"
				}
				if mode == "activation-secret-default" {
					prompt["default"] = "must-not-store"
				}
				bundle["activation"] = setup
			case "escape":
				exports["instructions"] = []string{"../../instructions/AGENTS.platform.md"}
			case "wrong-name":
				bundle["name"] = "other"
			case "missing-skill":
				exports["skills"] = []string{"absent"}
			case "missing-mcp":
				exports["mcp"].(map[string]any)["servers"] = []string{"obsidian"}
			case "unknown-export":
				exports["skillz"] = []string{"graphify"}
			case "duplicate-server":
				if err := AtomicWrite(a.DeliveryPath("bundles/graphify/mcp.json"), []byte(`{"mcpServers":{"graphify":{},"playwright":{}}}`), 0644); err != nil {
					t.Fatal(err)
				}
			case "non-object-server":
				if err := AtomicWrite(a.DeliveryPath("bundles/graphify/mcp.json"), []byte(`{"mcpServers":{"graphify":"wrong"}}`), 0644); err != nil {
					t.Fatal(err)
				}
			}
			data, err = json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			if err = AtomicWrite(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err = a.DeliveryCatalog(); err == nil {
				t.Fatal("invalid bundle accepted", mode)
			}
		})
	}
}
func TestSelectedCapabilitiesRejectToolAndSkillConflicts(t *testing.T) {
	a := &App{Platform: testutil.Delivery(t)}
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	graphify := catalog.Capabilities["graphify"]
	for _, mode := range []string{"tool", "skill"} {
		custom := DeliveryCapability{Instructions: []string{"instructions/AGENTS.platform.md"}}
		if mode == "tool" {
			custom.Tools = append([]DeliveryTool{}, graphify.Tools...)
			custom.Tools[0].Version = "0.9.81"
		} else {
			source := "custom/graphify"
			if err := AtomicWrite(a.DeliveryPath(source+"/SKILL.md"), []byte("---\nname: graphify\n---\ncustom"), 0644); err != nil {
				t.Fatal(err)
			}
			custom.Skills = []string{source}
		}
		catalog.Capabilities["custom"] = custom
		data, err := json.Marshal(catalog)
		if err != nil {
			t.Fatal(err)
		}
		if err = AtomicWrite(a.DeliveryPath("catalog.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err = a.ProjectCapabilities(Project{RuntimePath: t.TempDir(), Capabilities: []string{"graphify", "custom"}}); err == nil {
			t.Fatal("conflicting exports accepted", mode)
		}
	}
}
