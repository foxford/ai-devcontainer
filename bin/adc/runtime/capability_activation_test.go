package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestCapabilityActivationAsksSavesReusesAndDelivers(t *testing.T) {
	a, p, _ := mcpFixture(t)
	p.Capabilities = []string{"obsidian"}
	t.Setenv("OBSIDIAN_API_KEY", "")
	fixture, _ := probeFixture(t, false)
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	setup := catalog.Capabilities["obsidian"].Activation
	if setup == nil || len(setup.Prompts) != 2 || !setup.Prompts[1].Secret {
		t.Fatal("bundle lacks real activation questions")
	}
	path := filepath.Join(p.RuntimePath, ".agents", "mcp.local.json")
	writeFixture(t, path, `{"keep":"setting","mcpServers":{"personal":{"command":"mine"}}}`)
	calls := []string{}
	input := func(prompt shared.ActivationPrompt, current string) (string, error) {
		calls = append(calls, prompt.Key)
		if prompt.Key == "url" {
			return fixture.URL, nil
		}
		return "fixture-key", nil
	}
	if err := a.configureActivation(p, setup, false, input); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "url,OBSIDIAN_API_KEY" {
		t.Fatal("wrong questions")
	}
	if strings.Contains(readFixture(t, path), "fixture-key") || !strings.Contains(readFixture(t, path), "personal") || !strings.Contains(readFixture(t, path), "setting") {
		t.Fatal("local settings lost or key in JSON")
	}
	if !strings.Contains(readFixture(t, filepath.Join(p.RuntimePath, ".gitignore")), "/.agents/mcp.secrets.env") {
		t.Fatal("secrets not excluded from git")
	}
	input = func(shared.ActivationPrompt, string) (string, error) {
		t.Fatal("saved settings prompted again")
		return "", nil
	}
	if err := a.configureActivation(p, setup, false, input); err != nil {
		t.Fatal(err)
	}
	if err := a.MCPDoctor(p, MCPOperation{Name: "obsidian"}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeliverMCP(p); err != nil {
		t.Fatal(err)
	}
	namespace := p.ID + "-" + shared.CheckoutID(p.RuntimePath) + "-obsidian"
	for _, target := range []string{filepath.Join(p.RuntimePath, ".mcp.json"), filepath.Join(p.RuntimePath, ".adc", "opencode.json")} {
		content := readFixture(t, target)
		if !strings.Contains(content, namespace) || !strings.Contains(content, "Bearer fixture-key") {
			t.Fatal("driver missing authenticated MCP")
		}
	}
	values, err := readSecrets(a.secretFile(p.RuntimePath, "project"))
	if err != nil || values["OBSIDIAN_API_KEY"] != "fixture-key" {
		t.Fatal("key not stored literally")
	}
	// Rotate through the same questions, preserving a different server.
	writeFixture(t, path, `{"mcpServers":{"personal":{"command":"mine"},"obsidian":{"url":"`+fixture.URL+`","headers":{"Authorization":"Bearer stale-literal","X-Personal":"keep"}}}}`)
	input = func(prompt shared.ActivationPrompt, current string) (string, error) {
		if prompt.Key == "url" {
			return current, nil
		}
		return "rotated-key", nil
	}
	if err := a.configureActivation(p, setup, true, input); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFixture(t, path), "personal") {
		t.Fatal("rotation changed unrelated server")
	}
	plan, err := a.MCPPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	headers := plan.resolved["obsidian"]["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer rotated-key" || headers["X-Personal"] != "keep" {
		t.Fatal("rotation retained old literal or lost personal header")
	}
}

func TestCapabilityActivationCancellationReadonlyAndDisabled(t *testing.T) {
	a, p, _ := mcpFixture(t)
	p.Capabilities = []string{"obsidian"}
	t.Setenv("OBSIDIAN_API_KEY", "")
	catalog, _ := a.shared().DeliveryCatalog()
	setup := catalog.Capabilities["obsidian"].Activation
	input := func(prompt shared.ActivationPrompt, current string) (string, error) {
		if prompt.Key == "url" {
			return "http://localhost:27123/mcp/", nil
		}
		return "", errors.New("cancelled")
	}
	if err := a.configureActivation(p, setup, false, input); err == nil {
		t.Fatal("cancel passed")
	}
	for _, name := range []string{"mcp.local.json", "mcp.secrets.env"} {
		if _, err := os.Stat(filepath.Join(p.RuntimePath, ".agents", name)); !os.IsNotExist(err) {
			t.Fatal("cancel wrote settings")
		}
	}
	p.ReadOnly = true
	if err := a.configureActivation(p, setup, false, input); !errors.Is(err, os.ErrPermission) {
		t.Fatal("read only activated")
	}
	p.ReadOnly = false
	writeFixture(t, filepath.Join(p.RuntimePath, ".agents", "mcp.local.json"), `{"mcpServers":{"obsidian":null}}`)
	if err := a.configureActivation(p, setup, false, input); err == nil {
		t.Fatal("explicit null silently reenabled")
	}
}

func TestMCPDoctorFixAsksAllMissingSecrets(t *testing.T) {
	a, p, _ := mcpFixture(t)
	t.Setenv("ADC_FIX_FIRST", "")
	t.Setenv("ADC_FIX_SECOND", "")
	fixture, _ := probeFixture(t, false)
	writeFixture(t, filepath.Join(p.RuntimePath, ".agents", "mcp.json"), `{"mcpServers":{"custom":{"url":"`+fixture.URL+`","headers":{"Authorization":"Bearer ${ADC_FIX_FIRST}"},"x-requires":["env:ADC_FIX_FIRST","env:ADC_FIX_SECOND"]}}}`)
	var asked []string
	input := func(prompt shared.ActivationPrompt, _ string) (string, error) {
		asked = append(asked, prompt.Key)
		if prompt.Key == "ADC_FIX_FIRST" {
			return "fixture-key", nil
		}
		return "second-value", nil
	}
	if err := a.configureMissingSecrets(p, "custom", input); err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, ",") != "ADC_FIX_FIRST,ADC_FIX_SECOND" {
		t.Fatal("not all missing secrets were asked")
	}
	if err := a.MCPDoctor(p, MCPOperation{Name: "custom"}); err != nil {
		t.Fatal(err)
	}
	asked = nil
	if err := a.configureMissingSecrets(p, "custom", input); err != nil || len(asked) > 0 {
		t.Fatal("configured secrets were asked again")
	}
}
