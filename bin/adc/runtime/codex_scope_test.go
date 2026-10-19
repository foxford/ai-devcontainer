package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestCodexScopePreservesPersonalTablesAndMultilineStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := `model = "keep"
instructions = """
[mcp_servers.owned]
enabled = true
"""
[mcp_servers.owned]
"enabled" = true # replace only this setting
command = "runner"
[mcp_servers.owned.env]
enabled = "preserve-env-value"
[mcp_servers.'literal'] # valid literal key
'enabled' = true
command = "literal-runner"
[mcp_servers."quoted"]
command = "quoted-runner"
[mcp_servers.owned_extra]
enabled = true
command = "personal-runner"
[mcp_servers.personal]
enabled = false
command = "personal-disabled"
`
	writeFixture(t, path, original)
	if err := disableCodexMCP(path, map[string]bool{"owned": true, "literal": true, "quoted": true}); err != nil {
		t.Fatal(err)
	}
	got := readFixture(t, path)
	want := strings.Replace(original, "[mcp_servers.owned]\n\"enabled\" = true # replace only this setting", "[mcp_servers.owned]\nenabled = false", 1)
	want = strings.Replace(want, "[mcp_servers.'literal'] # valid literal key\n'enabled' = true", "[mcp_servers.'literal'] # valid literal key\nenabled = false", 1)
	want = strings.Replace(want, "[mcp_servers.\"quoted\"]\n", "[mcp_servers.\"quoted\"]\nenabled = false\n", 1)
	if got != want {
		t.Fatalf("scope edit changed personal configuration or missed an owned server:\n%s", got)
	}
	if err := disableCodexMCP(path, map[string]bool{"owned": true, "literal": true, "quoted": true}); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, path) != got {
		t.Fatal("repeated scoping changed the configuration")
	}
}

func TestCodexScopeActivatesOnlyOwnServersAndKeepsProjectConfiguration(t *testing.T) {
	a, first, store := mcpFixture(t)
	installCodexDriverFixture(t)
	home, _ := os.UserHomeDir()
	second := shared.Project{ID: "other", RuntimePath: t.TempDir()}
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner"}}}`)
	writeFixture(t, codexConfigPath(home), "[mcp_servers.personal]\ncommand = \"personal-runner\"\nenabled = true\n")
	projectPath := filepath.Join(first.RuntimePath, ".codex", "config.toml")
	personalProject := "# personal project settings\nmodel = \"keep\"\n[mcp_servers.project_personal]\ncommand = \"mine\"\n"
	writeFixture(t, projectPath, personalProject)
	for _, project := range []shared.Project{first, second, first} {
		if err := a.DeliverMCP(project); err != nil {
			t.Fatal(err)
		}
	}
	firstName := first.ID + "-" + shared.CheckoutID(first.RuntimePath) + "-service"
	secondName := second.ID + "-" + shared.CheckoutID(second.RuntimePath) + "-service"
	global := readFixture(t, codexConfigPath(home))
	for _, name := range []string{firstName, secondName} {
		if !strings.Contains(global, fmt.Sprintf("[mcp_servers.%q]\nenabled = false\n", name)) {
			t.Fatalf("owned server %s remains active globally:\n%s", name, global)
		}
	}
	if !strings.Contains(global, "[mcp_servers.personal]\ncommand = \"personal-runner\"\nenabled = true") {
		t.Fatal("personal global server was changed")
	}
	for _, pair := range [][3]string{{projectPath, firstName, secondName}, {filepath.Join(second.RuntimePath, ".codex", "config.toml"), secondName, firstName}} {
		content := readFixture(t, pair[0])
		if !strings.Contains(content, fmt.Sprintf("[mcp_servers.%q]\nenabled = true\n", pair[1])) || strings.Contains(content, pair[2]) {
			t.Fatalf("checkout selector leaks another checkout or omits its own server:\n%s", content)
		}
		if strings.Contains(content, "runner") && pair[0] != projectPath {
			t.Fatal("project selector contains a transport definition")
		}
	}
	if !strings.HasPrefix(readFixture(t, projectPath), personalProject) {
		t.Fatal("personal project configuration was changed")
	}
}

func TestCodexScopeMigratesOtherRegisteredCheckoutsOnSingleDelivery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the runtime registry uses Linux checkout paths")
	}
	for _, stateFile := range []string{".adc/mcp-state.json", ".adc/mcp-state", ".ai-devcontainer/mcp-state.json", ".ai-devcontainer/mcp-state"} {
		t.Run(stateFile, func(t *testing.T) {
			a, current, store := mcpFixture(t)
			installCodexDriverFixture(t)
			home, _ := os.UserHomeDir()
			current.Path = current.RuntimePath
			other := shared.Project{ID: "other", Path: t.TempDir(), ReadOnly: true}
			other.RuntimePath = other.Path
			if err := a.shared().Save(shared.Config{Version: shared.Schema, Name: "fixture", Port: 6768, Projects: []shared.Project{current, other}}); err != nil {
				t.Fatal(err)
			}
			foreign := other.ID + "-" + shared.CheckoutID(other.RuntimePath) + "-service"
			manual := other.ID + "-" + shared.CheckoutID(other.RuntimePath) + "-manual"
			state := foreign + "\n" + manual + "\n"
			if strings.HasSuffix(stateFile, ".json") {
				data, err := json.Marshal(mcpState{Version: 1, Servers: []string{foreign, manual}})
				if err != nil {
					t.Fatal(err)
				}
				state = string(data)
			}
			statePath := filepath.Join(other.RuntimePath, filepath.FromSlash(stateFile))
			writeFixture(t, statePath, state)
			manualConfig := fmt.Sprintf("[mcp_servers.%q]\nurl = \"https://manual.invalid/mcp\"\n[mcp_servers.%q.http_headers]\nAuthorization = \"personal-key\"\n", manual, manual)
			writeFixture(t, codexConfigPath(home), fmt.Sprintf("[mcp_servers.%q]\ncommand = \"old-runner\"\nenabled = true\n", foreign)+manualConfig)
			writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner"}}}`)
			if err := a.DeliverMCP(current); err != nil {
				t.Fatal(err)
			}
			global := readFixture(t, codexConfigPath(home))
			if !strings.Contains(global, fmt.Sprintf("[mcp_servers.%q]\nenabled = false\ncommand = \"old-runner\"", foreign)) {
				t.Fatalf("single-project sync left the registered neighbour globally enabled:\n%s", global)
			}
			if !strings.Contains(global, manualConfig) {
				t.Fatal("manual HTTP server was claimed by migration")
			}
			if readFixture(t, statePath) != state {
				t.Fatal("migration changed a read-only neighbour's state")
			}
			if _, err := os.Stat(filepath.Join(other.RuntimePath, ".codex")); !os.IsNotExist(err) {
				t.Fatal("migration wrote configuration into a read-only neighbour", err)
			}
		})
	}
}

func TestCodexScopeRejectsMalformedProjectBlockBeforeAgentWrites(t *testing.T) {
	a, p, store := mcpFixture(t)
	log := installCodexDriverFixture(t)
	home, _ := os.UserHomeDir()
	projectPath := filepath.Join(p.RuntimePath, ".codex", "config.toml")
	project := "model = \"keep\"\n" + codexScopeStart + "\n"
	global := "[mcp_servers.personal]\ncommand = \"keep\"\n"
	writeFixture(t, projectPath, project)
	writeFixture(t, codexConfigPath(home), global)
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner"}}}`)
	if err := a.DeliverMCP(p); err == nil {
		t.Fatal("malformed managed selector was accepted")
	}
	if readFixture(t, projectPath) != project || readFixture(t, codexConfigPath(home)) != global {
		t.Fatal("invalid selector changed Codex configuration")
	}
	for _, path := range []string{log, filepath.Join(p.RuntimePath, ".mcp.json"), filepath.Join(p.RuntimePath, ".adc", "mcp-state.json"), filepath.Join(home, ".hermes", "config.yaml")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("agent write occurred before selector validation", path, err)
		}
	}
}

// Native config loading is the contract here: JSON-file assertions alone cannot
// detect a broken project override, an inherited server, or a missing transport.
func TestCodexScopeNativeCheckoutLifecycle(t *testing.T) {
	binary := os.Getenv("ADC_NATIVE_CODEX")
	if binary == "" {
		t.Skip("set ADC_NATIVE_CODEX to verify native Codex scope loading")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the runtime registry uses Linux checkout paths")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("ADC_NATIVE_CODEX must name an absolute executable")
	}
	originalPath := os.Getenv("PATH")
	a, first, store := mcpFixture(t)
	tools := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(tools, "codex")); err != nil {
		t.Fatal(err)
	}
	// npm's executable can be a node wrapper, and worktree discovery needs git.
	t.Setenv("PATH", tools+string(os.PathListSeparator)+originalPath)
	first.RuntimePath = checkoutRepository(t)
	first.Path = first.RuntimePath
	second := shared.Project{ID: "other", RuntimePath: checkoutRepository(t)}
	second.Path = second.RuntimePath
	worktree := first
	worktree.RuntimePath = checkoutWorktree(t, first.RuntimePath, "review with spaces")
	if err := a.shared().Save(shared.Config{Version: shared.Schema, Name: "fixture", Port: 6768, Projects: []shared.Project{first, second}}); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	var global strings.Builder
	global.WriteString("[mcp_servers.personal]\ncommand = \"personal-fixture\"\n")
	for _, project := range []shared.Project{first, second, worktree} {
		fmt.Fprintf(&global, "[projects.%q]\ntrust_level = \"trusted\"\n", project.RuntimePath)
	}
	writeFixture(t, codexConfigPath(home), global.String())
	writeDefinition := func(key string) {
		writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"url":"http://127.0.0.1:9/mcp","headers":{"Authorization":"Bearer `+key+`"}}}}`)
	}
	writeDefinition("first-fixture-key")
	for _, project := range []shared.Project{first, second, worktree} {
		if err := a.DeliverMCP(project); err != nil {
			t.Fatal(err)
		}
	}
	name := func(project shared.Project) string {
		return project.ID + "-" + shared.CheckoutID(project.RuntimePath) + "-service"
	}
	check := func(root string, own string) {
		t.Helper()
		cmd := exec.Command(binary, "mcp", "list", "--json")
		cmd.Dir = root
		output, err := cmd.Output()
		if err != nil {
			if failure, ok := err.(*exec.ExitError); ok {
				t.Fatalf("native config loading in %s: %v\n%s", root, err, failure.Stderr)
			}
			t.Fatal(err)
		}
		var servers []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(output, &servers); err != nil {
			t.Fatal(err)
		}
		active := map[string]bool{}
		for _, server := range servers {
			if server.Enabled {
				active[server.Name] = true
			}
		}
		want := map[string]bool{"personal": true}
		if own != "" {
			want[own] = true
		}
		if !reflect.DeepEqual(active, want) {
			t.Fatalf("native active servers in %s = %v; want %v", root, active, want)
		}
	}
	outside := t.TempDir()
	checkAll := func(firstEnabled bool) {
		t.Helper()
		firstName := ""
		if firstEnabled {
			firstName = name(first)
		}
		check(first.RuntimePath, firstName)
		check(second.RuntimePath, name(second))
		check(worktree.RuntimePath, name(worktree))
		check(outside, "")
	}
	checkAll(true)
	// Rotation removes and recreates the global table. Inheriting this cwd for
	// admin commands would make the second command fail on a missing transport.
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(first.RuntimePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Error(err)
		}
	})
	writeDefinition("rotated-fixture-key")
	if err := a.DeliverMCP(first); err != nil {
		t.Fatal("rotation from inside a scoped checkout failed", err)
	}
	checkAll(true)
	cmd := exec.Command(binary, "mcp", "get", name(first), "--json")
	cmd.Dir = first.RuntimePath
	output, err := cmd.Output()
	if err != nil || !strings.Contains(string(output), "rotated-fixture-key") || strings.Contains(string(output), "first-fixture-key") {
		t.Fatal("native server did not load the rotated key", err)
	}
	for _, project := range []shared.Project{first, second, worktree} {
		selector := readFixture(t, filepath.Join(project.RuntimePath, ".codex", "config.toml"))
		if strings.Contains(selector, "fixture-key") || strings.Contains(selector, "http://") {
			t.Fatal("project configuration received credentials or transport settings")
		}
	}
	writeFixture(t, filepath.Join(first.RuntimePath, ".agents", "mcp.local.json"), `{"mcpServers":{"service":null}}`)
	if err := a.DeliverMCP(first); err != nil {
		t.Fatal("removal from inside a scoped checkout failed", err)
	}
	checkAll(false)
	if strings.Contains(readFixture(t, filepath.Join(first.RuntimePath, ".codex", "config.toml")), name(first)) {
		t.Fatal("removed server left a transportless project selector")
	}
}

func TestCodexScopeNativePreservesInlineManualHeaders(t *testing.T) {
	binary := os.Getenv("ADC_NATIVE_CODEX")
	if binary == "" {
		t.Skip("set ADC_NATIVE_CODEX to verify native inline TOML preservation")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("ADC_NATIVE_CODEX must name an absolute executable")
	}
	originalPath := os.Getenv("PATH")
	a, project, store := mcpFixture(t)
	tools := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(tools, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+originalPath)
	home, _ := os.UserHomeDir()
	name := project.ID + "-" + shared.CheckoutID(project.RuntimePath) + "-manual"
	original := fmt.Sprintf("[mcp_servers]\n%q = { url = \"https://manual.invalid/mcp\", http_headers = { Authorization = \"personal-inline-key\" } }\n", name)
	writeFixture(t, codexConfigPath(home), original)
	writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"manual":{"url":"https://automatic.invalid/mcp","headers":{"Authorization":"Bearer managed-key"}}}}`)
	for i := 0; i < 2; i++ {
		if err := a.DeliverMCP(project); err != nil {
			t.Fatal("delivery rejected a valid manual inline server", err)
		}
		if readFixture(t, codexConfigPath(home)) != original {
			t.Fatal("delivery changed a manual inline HTTP server")
		}
	}
	cmd := exec.Command(binary, "mcp", "get", name, "--json")
	cmd.Dir = t.TempDir()
	output, err := cmd.Output()
	if err != nil || !strings.Contains(string(output), "personal-inline-key") || strings.Contains(string(output), "managed-key") {
		t.Fatal("native Codex failed to preserve manual inline credentials", err)
	}
	if data, err := os.ReadFile(filepath.Join(project.RuntimePath, ".codex", "config.toml")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if strings.Contains(string(data), name) {
		t.Fatal("manual inline server was claimed by the project selector")
	}
}

func TestCodexScopePreservesManualProjectSelector(t *testing.T) {
	for _, fixture := range []struct{ name, config string }{
		{"table", "[mcp_servers.owned]\ncommand = \"manual-fixture\"\nenabled = false\n"},
		{"inline", "[mcp_servers]\nowned = { command = \"manual-fixture\", enabled = false }\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFixture(t, path, fixture.config)
			result, err := codexProjectConfig(path, []string{"owned"})
			if err != nil {
				t.Fatal(err)
			}
			if string(result) != fixture.config {
				t.Fatalf("generated scope changed or duplicated a manual server:\n%s", result)
			}
		})
	}
}

func TestCodexScopePreservesDottedManualHeaders(t *testing.T) {
	for _, key := range []string{"http_headers.Authorization", `"http_headers"."Authorization"`, "'http_headers'.'Authorization'"} {
		t.Run(key, func(t *testing.T) {
			a, project, store := mcpFixture(t)
			installCodexDriverFixture(t)
			home, _ := os.UserHomeDir()
			name := project.ID + "-" + shared.CheckoutID(project.RuntimePath) + "-manual"
			original := fmt.Sprintf("[mcp_servers.%q]\nurl = \"https://manual.invalid/mcp\"\n%s = \"personal-key\"\n", name, key)
			writeFixture(t, codexConfigPath(home), original)
			writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"manual":{"url":"https://automatic.invalid/mcp","headers":{"Authorization":"Bearer managed-key"}}}}`)
			for i := 0; i < 2; i++ {
				if err := a.DeliverMCP(project); err != nil {
					t.Fatal(err)
				}
			}
			if readFixture(t, codexConfigPath(home)) != original {
				t.Fatal("manual dotted headers were changed or taken over by ADC")
			}
			state, _, err := readMCPState(project.RuntimePath)
			private, privateErr := decodeCodexState(state.forDriver("codex"))
			if err != nil || privateErr != nil || shared.Contains(private.Headers, name) {
				t.Fatal("manual dotted headers became managed", err)
			}
		})
	}
}

func TestCodexScopeRejectsRootInlineBeforeAgentWrites(t *testing.T) {
	for _, location := range []string{"global", "project"} {
		t.Run(location, func(t *testing.T) {
			a, project, store := mcpFixture(t)
			log := installCodexDriverFixture(t)
			home, _ := os.UserHomeDir()
			globalPath := codexConfigPath(home)
			projectPath := filepath.Join(project.RuntimePath, ".codex", "config.toml")
			contents := map[string]string{
				globalPath:  "[mcp_servers.personal]\ncommand = \"keep\"\n",
				projectPath: "model = \"keep\"\n",
			}
			unsupportedPath := globalPath
			if location == "project" {
				unsupportedPath = projectPath
			}
			contents[unsupportedPath] = "mcp_servers = { personal = { command = \"keep\" } }\n"
			for path, content := range contents {
				writeFixture(t, path, content)
			}
			writeFixture(t, filepath.Join(store, "servers.json"), `{"mcpServers":{"service":{"command":"runner"}}}`)
			err := a.DeliverMCP(project)
			if err == nil || !strings.Contains(err.Error(), unsupportedPath) || !strings.Contains(err.Error(), "mcp_servers") {
				t.Fatal("unsupported root inline table did not identify its source", err)
			}
			for path, content := range contents {
				if readFixture(t, path) != content {
					t.Fatal("rejected delivery changed existing Codex configuration", path)
				}
			}
			for _, path := range []string{log, filepath.Join(project.RuntimePath, ".mcp.json"), filepath.Join(project.RuntimePath, ".adc", "mcp-state.json"), filepath.Join(home, ".hermes", "config.yaml")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("agent write occurred before root inline validation", path, err)
				}
			}
		})
	}
}

func TestCodexScopeFormatDistinguishesInlineTablesFromExamples(t *testing.T) {
	for _, fixture := range []struct {
		name, config string
		reject       bool
	}{
		{"bare", "mcp_servers = {}\n", true},
		{"quoted", "\"mcp_servers\" = {}\n", true},
		{"literal-key", "'mcp_servers' = {}\n", true},
		{"basic-string", "instructions = \"\"\"\nmcp_servers = {}\n[mcp_servers.example]\n\"\"\"\n", false},
		{"literal-string", "instructions = '''\nmcp_servers = {}\n[mcp_servers.example]\n'''\n", false},
		{"after-string", "instructions = \"\"\"\n[mcp_servers.example]\n\"\"\"\nmcp_servers = {}\n", true},
		{"comment", "# mcp_servers = {}\n", false},
		{"other-table", "[other]\nmcp_servers = {}\n", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			err := checkCodexScopeFormat("fixture.toml", []byte(fixture.config))
			if (err != nil) != fixture.reject {
				t.Fatalf("root inline validation = %v, want rejection=%v", err, fixture.reject)
			}
		})
	}
}
