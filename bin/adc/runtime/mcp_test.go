package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
	"go.yaml.in/yaml/v3"
)

func mcpFixture(t *testing.T) (*App, shared.Project, string) {
	t.Helper()
	home := testutil.Home(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	store := t.TempDir()
	t.Setenv("AI_DEVCONTAINER_MCP_STORE", store)
	// Agent config tests must not invoke a CLI installed on the developer's machine.
	t.Setenv("PATH", t.TempDir())
	a := &App{Platform: testutil.Delivery(t), State: filepath.Join(home, ".adc"), Runtime: true}
	return a, shared.Project{ID: "app", RuntimePath: t.TempDir()}, store
}

func TestMCPLayersMergeDisableGatesAndRedaction(t *testing.T) {
	a, p, store := mcpFixture(t)
	t.Setenv("ADC_TEST_MCP_VALUE", "environment-value")
	t.Setenv("ADC_TEST_MCP_MISSING", "")
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"custom":{"command":"runner","args":["user"],"env":{"ADC_TEST_MCP_VALUE":"${ADC_TEST_MCP_VALUE}","KEEP":"user"}},"disabled":{"command":"runner"},"reenabled":null,"gated":{"command":"runner","x-requires":["env:ADC_TEST_MCP_MISSING"]},"missing-path":{"command":"runner","x-requires":["path:${REPO_ROOT}/not-here"]}}}`)
	writeFixture(t, filepath.Join(store, "secrets.env"), "ADC_TEST_MCP_VALUE=user-secret\n")
	writeFixture(t, filepath.Join(p.RuntimePath, ".agents", "mcp.json"), `{"mcpServers":{"custom":{"args":["project"],"env":{"EXTRA":"project"},"headers":{"Authorization":"Bearer literal-secret-value"}},"disabled":null,"reenabled":{"command":"runner"}}}`)
	writeFixture(t, filepath.Join(p.RuntimePath, ".agents", "mcp.secrets.env"), "ADC_TEST_MCP_VALUE=project-secret\n")
	writeFixture(t, filepath.Join(p.RuntimePath, ".agents", "mcp.local.json"), `{"mcpServers":{"custom":{"env":{"EXTRA":"local"},"cwd":"${REPO_ROOT}"}}}`)
	plan, err := a.MCPPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	server := plan.resolved["custom"]
	if server["command"] != "runner" || !reflect.DeepEqual(server["args"], []any{"project"}) || server["cwd"] != p.RuntimePath {
		t.Fatalf("recursive merge failed: %#v", server)
	}
	if !reflect.DeepEqual(server["env"], map[string]any{"ADC_TEST_MCP_VALUE": "project-secret", "KEEP": "user", "EXTRA": "local"}) {
		t.Fatalf("wrong secret or nested precedence: %#v", server["env"])
	}
	if plan.Disabled["disabled"] != "project" || plan.Disabled["reenabled"] != "" || plan.resolved["reenabled"] == nil {
		t.Fatal("null disable/re-enable precedence failed")
	}
	if plan.Reasons["gated"].NeedEnv != "ADC_TEST_MCP_MISSING" || plan.Reasons["missing-path"].NeedPath != p.RuntimePath+"/not-here" {
		t.Fatalf("gate diagnostics missing: %#v", plan.Reasons)
	}
	if plan.Origins["custom"].Scope != "local" || !reflect.DeepEqual(plan.Origins["custom"].Shadows, []string{"user", "project"}) {
		t.Fatalf("incorrect provenance: %#v", plan.Origins)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"project-secret", "user-secret", "literal-secret-value"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("public plan leaked %q", secret)
		}
	}
	if !strings.Contains(string(data), "${ADC_TEST_MCP_VALUE}") {
		t.Fatal("public plan lost the declared placeholder")
	}
	if _, ok := plan.Defs["playwright"]; ok {
		t.Fatal("unselected platform capability delivered")
	}
	p.Capabilities = []string{"frontend"}
	plan, err = a.MCPPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.Defs["playwright"]; !ok {
		t.Fatal("selected frontend capability did not include Playwright")
	}
	if _, ok := plan.Defs["chrome-devtools"]; !ok {
		t.Fatal("selected frontend capability did not include Chrome DevTools")
	}
}

func TestMCPSecretsAreLiteralAndDuplicateKeysAreReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	writeFixture(t, path, "# keep comment\nexport TOKEN=old\nTOKEN='last'\nOTHER=untouched\n")
	value := `$(touch should-never-be-created); "quoted"; $HOME = literal`
	if err := setSecret(path, "TOKEN", value); err != nil {
		t.Fatal(err)
	}
	values, err := readSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["TOKEN"] != value || values["OTHER"] != "untouched" || strings.Count(readFixture(t, path), "TOKEN=") != 1 {
		t.Fatalf("secret not preserved literally: %#v", values)
	}
	if !strings.Contains(readFixture(t, path), "# keep comment") {
		t.Fatal("comments removed")
	}
	for _, pair := range [][2]string{{"INVALID-NAME", "x"}, {"TOKEN", "multiline\nvalue"}} {
		before := readFixture(t, path)
		if err := setSecret(path, pair[0], pair[1]); err == nil || readFixture(t, path) != before {
			t.Fatal("invalid secret was accepted or changed the file")
		}
	}
}

func TestMCPProjectAddExtractsSecretAndDefaultsToLocal(t *testing.T) {
	a, p, _ := mcpFixture(t)
	if err := a.MCP(p, MCPOperation{Action: "add", Name: "service", Scope: "project", URL: "https://example.invalid/mcp", Headers: []string{"Authorization: Bearer test-token-123456"}}); err != nil {
		t.Fatal(err)
	}
	projectFile := filepath.Join(p.RuntimePath, ".agents", "mcp.json")
	data := readFixture(t, projectFile)
	if strings.Contains(data, "test-token-123456") || !strings.Contains(data, "Bearer ${SERVICE_AUTHORIZATION}") {
		t.Fatalf("project JSON contains a literal secret or lost its scheme: %s", data)
	}
	values, err := readSecrets(filepath.Join(p.RuntimePath, ".agents", "mcp.secrets.env"))
	if err != nil || values["SERVICE_AUTHORIZATION"] != "test-token-123456" {
		t.Fatalf("secret extraction failed: %#v %v", values, err)
	}
	if err := a.MCP(p, MCPOperation{Action: "add", Name: "local-service", Command: []string{"runner", "--flag", "value with spaces"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFixture(t, filepath.Join(p.RuntimePath, ".agents", "mcp.local.json")), "local-service") || strings.Contains(readFixture(t, projectFile), "local-service") {
		t.Fatal("default add did not use the local scope")
	}
	if err := a.MCP(p, MCPOperation{Action: "disable", Name: "local-service"}); err != nil {
		t.Fatal(err)
	}
	plan, err := a.MCPPlan(p)
	if err != nil || plan.Disabled["local-service"] != "local" {
		t.Fatalf("disable failed: %#v %v", plan.Disabled, err)
	}
	if err := a.MCP(p, MCPOperation{Action: "add", Name: "forbidden", Scope: "global", Command: []string{"runner"}}); err == nil {
		t.Fatal("allowed modification of platform scope")
	}
	p.ReadOnly = true
	if err := a.MCP(p, MCPOperation{Action: "add", Name: "forbidden", Command: []string{"runner"}}); err == nil {
		t.Fatal("allowed write into read-only project")
	}
}

func TestMCPDeliveryPreservesOtherCheckoutsAndForeignClaude(t *testing.T) {
	a, p, store := mcpFixture(t)
	home, _ := os.UserHomeDir()
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner","args":["a"]}}}`)
	writeFixture(t, filepath.Join(home, ".hermes", "config.yaml"), "# personal configuration\nmodel: keep\nmcp_servers:\n  personal:\n    command: personal-cli\n")
	writeFixture(t, filepath.Join(home, ".dsh", "cordis.patch.yml"), "# personal patches\n[]\n")
	foreign := `{"mcpServers":{"mine":{"command":"personal-cli"}}}`
	writeFixture(t, filepath.Join(p.RuntimePath, ".mcp.json"), foreign)
	other := shared.Project{ID: "other", RuntimePath: t.TempDir()}
	for _, project := range []shared.Project{p, other, p} {
		if err := a.DeliverMCP(project); err != nil {
			t.Fatal(err)
		}
	}
	if readFixture(t, filepath.Join(p.RuntimePath, ".mcp.json")) != foreign {
		t.Fatal("foreign Claude file overwritten on first or repeated sync")
	}
	var config map[string]any
	if err := yaml.Unmarshal([]byte(readFixture(t, filepath.Join(home, ".hermes", "config.yaml"))), &config); err != nil {
		t.Fatal(err)
	}
	servers := config["mcp_servers"].(map[string]any)
	ours, theirs := p.ID+"-"+shared.CheckoutID(p.RuntimePath)+"-service", other.ID+"-"+shared.CheckoutID(other.RuntimePath)+"-service"
	if config["model"] != "keep" || servers["personal"] == nil || servers[ours] == nil || servers[theirs] == nil {
		t.Fatalf("shared home lost another project's or personal settings: %#v", config)
	}
	writeFixture(t, filepath.Join(p.RuntimePath, ".agents", "mcp.local.json"), `{"mcpServers":{"service":null}}`)
	if err := a.DeliverMCP(p); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(readFixture(t, filepath.Join(home, ".hermes", "config.yaml"))), &config); err != nil {
		t.Fatal(err)
	}
	servers = config["mcp_servers"].(map[string]any)
	if servers[ours] != nil || servers[theirs] == nil || servers["personal"] == nil {
		t.Fatal("cleanup removed the wrong checkout's servers")
	}
	patch := readFixture(t, filepath.Join(home, ".dsh", "cordis.patch.yml"))
	if !strings.Contains(patch, "# personal patches") || !strings.Contains(patch, theirs) || strings.Contains(patch, ours) {
		t.Fatalf("DSH patch ownership failed: %s", patch)
	}
}

func TestMCPRejectsInvalidAgentYAMLBeforeWriting(t *testing.T) {
	for _, bad := range []struct{ path, value string }{{".hermes/config.yaml", "mcp_servers: []\n"}, {".dsh/cordis.patch.yml", "not-a-list: true\n"}, {".dsh/cordis.patch.yml", "- insert: [\n"}} {
		t.Run(bad.path+bad.value, func(t *testing.T) {
			a, p, store := mcpFixture(t)
			home, _ := os.UserHomeDir()
			path := filepath.Join(home, filepath.FromSlash(bad.path))
			writeFixture(t, path, bad.value)
			writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner"}}}`)
			if err := a.DeliverMCP(p); err == nil {
				t.Fatal("invalid agent configuration was accepted")
			}
			if readFixture(t, path) != bad.value {
				t.Fatal("invalid original file was changed")
			}
			if _, err := os.Stat(filepath.Join(p.RuntimePath, ".mcp.json")); !os.IsNotExist(err) {
				t.Fatal("Claude config written before YAML validation")
			}
		})
	}
}

func TestMCPDSHInsertTransportAndOAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patch.yml")
	writeFixture(t, path, "# keep\n[]\n")
	data, err := dshMCP(path, "checkout", "/repo", map[string]MCPServer{
		"stdio": {"command": "runner", "args": []any{"arg"}, "env": map[string]any{"VAR": "value"}},
		"http":  {"url": "https://example.invalid/mcp", "headers": map[string]any{"Authorization": "Bearer value"}},
		"oauth": {"url": "https://oauth.invalid"},
	}, map[string]bool{"oauth": true})
	if err != nil {
		t.Fatal(err)
	}
	var patches []struct {
		Insert []struct {
			ID, Name string
			Config   map[string]any
		}
	}
	if err := yaml.Unmarshal(data, &patches); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("expected two non-OAuth patches: %s", data)
	}
	for _, patch := range patches {
		if len(patch.Insert) != 1 || patch.Insert[0].Name != "@deepseek-ai/dsh-mcp-client" {
			t.Fatalf("unexpected Cordis insert: %#v", patch)
		}
		item := patch.Insert[0]
		switch item.ID {
		case "ai-devcontainer-mcp-stdio":
			if item.Config["cwd"] != "/repo" || item.Config["transport"] != "stdio" || item.Config["command"] != "runner" {
				t.Fatal("stdio transport lost command or checkout cwd")
			}
		case "ai-devcontainer-mcp-http":
			if item.Config["transport"] != "streamable-http" || item.Config["headers"] == nil {
				t.Fatal("HTTP transport lost headers")
			}
		default:
			t.Fatal("unexpected patch", item.ID)
		}
	}
}

func TestMCPSecretsReplaceSymlinkWithoutChangingSource(t *testing.T) {
	requireSymlinks(t)
	a, p, _ := mcpFixture(t)
	source := filepath.Join(t.TempDir(), "source.env")
	writeFixture(t, source, "TOKEN=keep-literal\n")
	if err := os.Chmod(source, 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.RuntimePath, ".agents", "mcp.secrets.env")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, path); err != nil {
		t.Fatal(err)
	}
	if err := a.DeliverMCP(p); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || readFixture(t, path) != "TOKEN=keep-literal\n" {
		t.Fatalf("secret symlink not replaced by a regular file: %v", err)
	}
	if runtime.GOOS != "windows" {
		original, err := os.Stat(source)
		if err != nil || original.Mode().Perm() != 0644 || info.Mode().Perm() != 0600 {
			t.Fatal("source permissions changed or secret file is not private")
		}
	}
}

func TestMCPDeliveryRetriesAfterCodexFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses the runtime's POSIX shell")
	}
	a, p, store := mcpFixture(t)
	tools := t.TempDir()
	fake := filepath.Join(tools, "codex")
	writeFixture(t, fake, "#!/bin/sh\nexit 13\n")
	if err := os.Chmod(fake, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner","args":["before"]}}}`)
	if err := a.DeliverMCP(p); err == nil {
		t.Fatal("external CLI failure was ignored")
	}
	state, exists, err := readMCPState(p.RuntimePath)
	if err != nil || !exists || !state.ClaudeOwned || len(state.Servers) != 1 {
		t.Fatalf("failed sync lost file ownership or server names: %#v %v", state, err)
	}
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner","args":["after"]}}}`)
	if err := os.Remove(fake); err != nil {
		t.Fatal(err)
	}
	if err := a.DeliverMCP(p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFixture(t, filepath.Join(p.RuntimePath, ".mcp.json")), "after") {
		t.Fatal("retry treated the previously generated Claude file as foreign")
	}
}
