package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// A native executable fixture gives all three CI platforms the same CLI contract.
func TestMain(m *testing.M) {
	if code, handled := graphBackgroundHelper(); handled {
		os.Exit(code)
	}
	if os.Getenv("ADC_CODEX_DRIVER_HELPER") == "1" {
		os.Exit(codexDriverHelper(os.Args[1:]))
	}
	os.Exit(m.Run())
}
func codexDriverHelper(args []string) int {
	if len(args) < 3 || args[0] != "mcp" {
		return 2
	}
	log := os.Getenv("ADC_CODEX_ARGS_LOG")
	f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 3
	}
	_ = json.NewEncoder(f).Encode(args)
	f.Close()
	if os.Getenv("ADC_CODEX_FAIL") == "1" {
		return 13
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codex", "config.toml")
	data, _ := os.ReadFile(path)
	name := args[2]
	keep := []string{}
	skip := false
	prefix := "mcp_servers." + name
	quoted := "mcp_servers.\"" + name + "\""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "[") {
			section := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			skip = section == prefix || strings.HasPrefix(section, prefix+".") || section == quoted || strings.HasPrefix(section, quoted+".")
		}
		if !skip {
			keep = append(keep, line)
		}
	}
	content := strings.Join(keep, "\n")
	if args[1] == "add" {
		q, _ := json.Marshal(name)
		content += fmt.Sprintf("\n[mcp_servers.%s]\n", q)
		if len(args) == 5 && args[3] == "--url" {
			u, _ := json.Marshal(args[4])
			content += fmt.Sprintf("url = %s\n", u)
		} else {
			content += "command = \"runner\"\n"
		}
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, []byte(content), 0600) != nil {
		return 4
	}
	return 0
}
func installCodexDriverFixture(t *testing.T) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	name := "codex"
	if filepath.Ext(binary) == ".exe" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(directory, name), data, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("ADC_CODEX_DRIVER_HELPER", "1")
	log := filepath.Join(t.TempDir(), "arguments.jsonl")
	t.Setenv("ADC_CODEX_ARGS_LOG", log)
	return log
}

func TestCodexDriverHeadersRotatePreserveManualAndRetry(t *testing.T) {
	a, p, store := mcpFixture(t)
	argsLog := installCodexDriverFixture(t)
	home, _ := os.UserHomeDir()
	config := filepath.Join(home, ".codex", "config.toml")
	namespace := p.ID + "-" + shared.CheckoutID(p.RuntimePath)
	manual := namespace + "-manual"
	writeFixture(t, config, "model = \"keep\"\n[mcp_servers.\""+manual+"\"]\nurl = \"http://manual.local/mcp/\"\n[mcp_servers.\""+manual+"\".http_headers]\nAuthorization = \"manual-header\"\n[mcp_servers.other]\ncommand = \"mine\"\n")
	definitions := func(secret string) {
		writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"url":"http://fixture.local/mcp/","headers":{"Authorization":"Bearer `+secret+`"}},"manual":{"url":"http://fixture.local/mcp/","headers":{"Authorization":"replace-not-allowed"}}}}`)
	}
	definitions("first-key")
	for i := 0; i < 2; i++ {
		if err := a.DeliverMCP(p); err != nil {
			t.Fatal(err)
		}
	}
	content := readFixture(t, config)
	if strings.Count(content, "Bearer first-key") != 1 || !strings.Contains(content, "manual-header") || strings.Contains(content, "replace-not-allowed") || !strings.Contains(content, "command = \"mine\"") {
		t.Fatal("duplicate headers or manual config lost")
	}
	if strings.Contains(readFixture(t, argsLog), "first-key") {
		t.Fatal("secret passed in CLI arguments")
	}
	definitions("second-key")
	t.Setenv("ADC_CODEX_FAIL", "1")
	if err := a.DeliverMCP(p); err == nil {
		t.Fatal("CLI failure ignored")
	}
	state, _, err := readMCPState(p.RuntimePath)
	private, privateErr := decodeCodexState(state.forDriver("codex"))
	if err != nil || privateErr != nil || !shared.Contains(private.Headers, namespace+"-service") || shared.Contains(private.Headers, manual) {
		t.Fatal("retry ownership includes foreign headers or lost managed headers")
	}
	t.Setenv("ADC_CODEX_FAIL", "")
	if err := a.DeliverMCP(p); err != nil {
		t.Fatal(err)
	}
	content = readFixture(t, config)
	if strings.Contains(content, "first-key") || strings.Count(content, "Bearer second-key") != 1 || !strings.Contains(content, "model = \"keep\"") {
		t.Fatal("rotation failed")
	}
	if strings.Contains(readFixture(t, argsLog), "second-key") {
		t.Fatal("rotation leaked into argv")
	}
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{}}`)
	if err := a.DeliverMCP(p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFixture(t, config), "second-key") || !strings.Contains(readFixture(t, config), "command = \"mine\"") || !strings.Contains(readFixture(t, config), "manual-header") {
		t.Fatal("stale managed entry or foreign config removed")
	}
}

func TestOpenCodeDriverIsolatesCheckoutsAndPreservesUserLayers(t *testing.T) {
	a, p, store := mcpFixture(t)
	home, _ := os.UserHomeDir()
	global := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	writeFixture(t, global, "// custom preferences\n{\"model\":\"mine\"}\n")
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"http":{"url":"http://fixture.local/mcp/","headers":{"Authorization":"Bearer private"}},"stdio":{"command":"runner","args":["serve"],"env":{"TOKEN":"private"}}}}`)
	if err := a.DeliverMCP(p); err != nil {
		t.Fatal(err)
	}
	other := shared.Project{ID: "other", RuntimePath: t.TempDir()}
	if err := a.DeliverMCP(other); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.RuntimePath, ".adc", "opencode.json")
	data := readFixture(t, path)
	if strings.Contains(data, "other-") || !strings.Contains(data, "\"remote\"") || !strings.Contains(data, "\"local\"") || !strings.Contains(data, "AGENTS.platform.md") {
		t.Fatal("native conversion or isolation failed")
	}
	if readFixture(t, global) != "// custom preferences\n{\"model\":\"mine\"}\n" {
		t.Fatal("global human config changed")
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"user/model","mcp":{"personal":{"type":"local","command":["mine"]}}}`)
	content, err := openCodeEnvironment(p.RuntimePath)
	if err != nil || !strings.Contains(content, "user/model") || !strings.Contains(content, "personal") || !strings.Contains(content, "Bearer private") {
		t.Fatal("inline config merge lost preferences or ADC servers")
	}
	drivers := agentDrivers()
	seen := map[string]bool{}
	for _, driver := range drivers {
		if seen[driver.Name()] {
			t.Fatal("duplicate agent driver")
		}
		seen[driver.Name()] = true
	}
	for _, name := range []string{"claude", "codex", "opencode"} {
		if !seen[name] {
			t.Fatal("required driver missing", name)
		}
	}
}

// Optional native CLI validation uses a completely isolated HOME/CODEX_HOME.
func TestCodexDriverNativeCLI(t *testing.T) {
	binary := os.Getenv("ADC_NATIVE_CODEX")
	if binary == "" {
		t.Skip("set ADC_NATIVE_CODEX to verify a native Codex CLI")
	}
	originalPath := os.Getenv("PATH")
	a, p, store := mcpFixture(t)
	home, _ := os.UserHomeDir()
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	directory := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(directory, "codex")); err != nil {
		t.Fatal(err)
	}
	// Installed Codex may use a Node launcher; retain its runtime dependencies.
	t.Setenv("PATH", directory+string(os.PathListSeparator)+originalPath)
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"native":{"url":"http://127.0.0.1:9/mcp/","headers":{"Authorization":"Bearer fixture-native-key"}}}}`)
	for i := 0; i < 2; i++ {
		if err := a.DeliverMCP(p); err != nil {
			t.Fatal(err)
		}
	}
	name := p.ID + "-" + shared.CheckoutID(p.RuntimePath) + "-native"
	output, err := exec.Command(binary, "mcp", "get", name, "--json").Output()
	if err != nil {
		t.Fatal("native Codex rejected its delivered TOML", err)
	}
	if !json.Valid(output) || !strings.Contains(string(output), "fixture-native-key") {
		t.Fatal("native CLI did not load HTTP headers")
	}
}
