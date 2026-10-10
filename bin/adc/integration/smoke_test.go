package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

// Opt in because this test builds an image and starts an isolated Docker workspace.
// It never mounts the developer's home or uses accounts or model APIs.
func TestDockerWorkspace(t *testing.T) {
	if os.Getenv("ADC_INTEGRATION") != "1" {
		t.Skip("set ADC_INTEGRATION=1 to test the real Docker runtime")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Docker workspace integration runs on Linux")
	}
	root := testutil.Root(t)
	tmp := t.TempDir()
	name := fmt.Sprintf("smoke-%d-%d", os.Getpid(), time.Now().UnixNano())
	container := "adc-" + name
	cli := filepath.Join(tmp, "adc")
	t.Setenv("ADC_HOME", filepath.Join(tmp, "state"))
	t.Setenv("AI_DEVCONTAINER_HOME", root)
	t.Setenv("ADC_INSTALL_AI_TOOLS", "0")
	t.Setenv("ADC_RUNTIME", "0")

	run := func(dir, stdin, binary string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir, cmd.Stdin = dir, strings.NewReader(stdin)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %q: %v\n%s", binary, args, err, data)
		}
		return string(data)
	}
	adc := func(args ...string) string { t.Helper(); return run(root, "", cli, args...) }
	docker := func(args ...string) string { t.Helper(); return run(root, "", "docker", args...) }
	execNode := func(args ...string) string {
		t.Helper()
		return docker(append([]string{"exec", "--user", "node", container}, args...)...)
	}
	assertContains := func(value, want string) {
		t.Helper()
		if !strings.Contains(value, want) {
			t.Fatalf("expected %q in:\n%s", want, value)
		}
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Diagnostics precede cleanup when a check fails.
	t.Cleanup(func() {
		if t.Failed() {
			cmd := exec.Command("docker", "logs", "--tail", "100", container)
			data, _ := cmd.CombinedOutput()
			t.Logf("runtime logs:\n%s", data)
		}
		for _, args := range [][]string{{"rm", "-f", container}, {"volume", "rm", container + "-home", container + "-tools", container + "-worktrees"}} {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cmd := exec.CommandContext(ctx, "docker", args...)
			data, err := cmd.CombinedOutput()
			cancel()
			if err != nil {
				t.Logf("cleanup: %v: %s", err, data)
			}
		}
	})
	run(root, "", "go", "build", "-o", cli, "./bin/adc")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	freePort := func() int {
		t.Helper()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		return port
	}
	omniPort, graphPort := freePort(), freePort()
	adc("workspace", "init", name, "--port", strconv.Itoa(port), "--omniroute-port", strconv.Itoa(omniPort), "--graph-port", strconv.Itoa(graphPort))
	for _, id := range []string{"app-a", "app-b"} {
		project := filepath.Join(tmp, id)
		if err := os.Mkdir(project, 0755); err != nil {
			t.Fatal(err)
		}
		run(project, "", "git", "init", "-q", "-b", "main")
		if err := os.MkdirAll(filepath.Join(project, ".ai-devcontainer", "skills-base", "example"), 0700); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(project, ".ai-devcontainer", "skills-base", "example", "SKILL.md"), "legacy fork base")
		write(filepath.Join(project, "AGENTS.md"), "Keep project rules\n")
		write(filepath.Join(project, "example.ts"), "export function authenticate(name: string) { return name.length > 0; }\nexport function login(name: string) { return authenticate(name); }\n")
		write(filepath.Join(project, "orca.yaml"), "# team setup\nscripts:\n  setup: printf 'ADC_TEAM_SETUP_OK\\n'\n")
		if err := os.Mkdir(filepath.Join(project, ".agents"), 0755); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(project, ".agents", "motd.sh"), "printf 'ADC_PROJECT_BANNER_OK\\n'\n")
		run(project, "", "git", "add", "AGENTS.md", "orca.yaml", ".agents/motd.sh", "example.ts")
		run(project, "", "git", "-c", "user.name=ADC-Smoke", "-c", "user.email=adc-smoke@example.invalid", "commit", "-qm", "fixture")
		args := []string{"project", "add", project, "--id", id}
		if id == "app-a" {
			args = append(args, "--capability", "changesets")
		}
		adc(args...)
	}
	t.Log("building and starting the real runtime")
	startup := adc("workspace", "start")
	assertContains(startup, "Ctrl+C прекращает только ожидание")
	assertContains(startup, "консоль свободна")
	assertContains(adc("workspace", "status"), "running (готов)")
	if inside, host := strings.TrimSpace(execNode("adc", "--version")), strings.TrimSpace(adc("--version")); inside != host {
		t.Fatalf("runtime revision %q differs from host %q", inside, host)
	}
	for _, id := range []string{"app-a", "app-b"} {
		project := filepath.Join(tmp, id)
		assertContains(read(filepath.Join(project, ".adc", "skills-base", "example", "SKILL.md")), "legacy fork base")
		assertContains(read(filepath.Join(project, ".ai-devcontainer", "skills-base", "example", "SKILL.md")), "legacy fork base")
	}
	execNode("bash", "-lc", `test "$(command -v codex)" = /opt/adc-agents/codex && test "$HISTFILE" = "$HOME/.history/.bash_history" && node --version && go version && rustc --version && bun --version`)
	execNode("zsh", "-lic", `test "$(command -v codex)" = /opt/adc-agents/codex && test "$HISTFILE" = "$HOME/.history/.zsh_history"`)
	var status struct {
		Result struct{ Runtime struct{ Reachable bool } }
	}
	decode(t, adc("orca", "status", "--json"), &status)
	if !status.Result.Runtime.Reachable {
		t.Fatal("Orca runtime is unreachable")
	}
	assertContains(read(filepath.Join(tmp, "app-a", "AGENTS.platform.md")), "Changesets")
	if strings.Contains(read(filepath.Join(tmp, "app-b", "AGENTS.platform.md")), "Changesets") {
		t.Fatal("capability from app-a leaked into app-b")
	}
	assertContains(read(filepath.Join(tmp, "app-a", "AGENTS.md")), "Keep project rules")
	execNode("test", "!", "-e", filepath.Join(tmp, "app-b", "MONOREPO.md"))
	adc("worktree", "create", "app-a", "smoke-worktree")
	var trees struct {
		Result struct {
			Worktrees []struct{ Path, DisplayName string }
		}
	}
	decode(t, adc("orca", "worktree", "list", "--json"), &trees)
	worktree := ""
	for _, tree := range trees.Result.Worktrees {
		if tree.DisplayName == "smoke-worktree" {
			worktree = tree.Path
		}
	}
	if worktree == "" {
		t.Fatal("Orca did not create the requested worktree")
	}
	execNode("test", "-f", filepath.Join(worktree, ".git"))
	var terminal struct {
		Result struct{ Terminal struct{ Handle string } }
	}
	decode(t, adc("orca", "terminal", "create", "--worktree", "path:"+worktree, "--command", `bash -ic 'printf "ADC_PTY_SMOKE_OK\n"'`, "--json"), &terminal)
	var tail string
	for attempt := 0; attempt < 20; attempt++ {
		var response struct {
			Result struct {
				Terminal struct{ Tail json.RawMessage }
			}
		}
		decode(t, adc("orca", "terminal", "read", "--terminal", terminal.Result.Terminal.Handle, "--limit", "200", "--json"), &response)
		var lines []string
		if err := json.Unmarshal(response.Result.Terminal.Tail, &lines); err == nil {
			tail = strings.Join(lines, "\n")
		} else if err := json.Unmarshal(response.Result.Terminal.Tail, &tail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(tail, "ADC_PTY_SMOKE_OK") && strings.Contains(tail, "ADC · "+name) && strings.Contains(tail, "ADC_PROJECT_BANNER_OK") {
			break
		}
		time.Sleep(time.Second)
	}
	assertContains(tail, "ADC_PTY_SMOKE_OK")
	assertContains(tail, "ADC · "+name)
	assertContains(tail, "ADC_PROJECT_BANNER_OK")
	execNode("test", "-f", filepath.Join(worktree, "AGENTS.platform.md"))
	execNode("rm", filepath.Join(worktree, "AGENTS.platform.md"))
	run(root, "#!/bin/sh\nprintf 'ADC_AGENT_STARTED\\n'\n", "docker", "exec", "-i", "--user", "node", container, "sh", "-c", "cat > /opt/ai-tools/bin/codex; chmod +x /opt/ai-tools/bin/codex")
	assertContains(docker("exec", "--user", "node", "--workdir", worktree, container, "codex"), "ADC_AGENT_STARTED")
	execNode("test", "-f", filepath.Join(worktree, "AGENTS.platform.md"))
	execNode("sh", "-c", `printf 'ADC_PERSIST_SMOKE\n' > "$HOME/adc-smoke-persist"; printf '# ADC_USER_SHELL_SETTINGS\n' >> "$HOME/.zshrc"; printf 'ADC_HISTORY_PERSIST\n' >> "$HOME/.history/.bash_history"`)

	var verifyBundles func()
	if os.Getenv("ADC_TOOL_BUNDLES_INTEGRATION") == "1" {
		t.Log("installing real Graphify and OmniRoute without agent accounts")
		execNode("adc", "tools", "install", "graphify")
		execNode("adc", "tools", "install", "omniroute")
		project := filepath.Join(tmp, "app-a")
		write(filepath.Join(project, ".agents", "project.json"), `{"capabilities":["graphify"]}`)
		synced := docker("exec", "--user", "node", "--env", "ADC_INSTALL_AI_TOOLS=1", container, "adc", "sync", "app-a")
		assertContains(synced, "Graphify: обновление")
		assertContains(read(filepath.Join(project, "graphify-out", "graph.json")), "authenticate")
		if _, err := os.Stat(filepath.Join(tmp, "app-b", "graphify-out")); !os.IsNotExist(err) {
			t.Fatal("graph leaked into unrelated project")
		}
		repeated := docker("exec", "--user", "node", "--env", "ADC_INSTALL_AI_TOOLS=1", container, "adc", "sync", "app-a")
		if strings.Contains(repeated, "Graphify: обновление") {
			t.Fatal("unchanged index rebuilt")
		}
		write(filepath.Join(project, "example.ts"), "export function authenticateNew(name: string) { return name.length > 0; }\nexport function login(name: string) { return authenticateNew(name); }\n")
		input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"graphify_query","arguments":{"question":"authenticateNew"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"graphify_explain","arguments":{"node":"authenticateNew"}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"graphify_path","arguments":{"from":"login","to":"authenticateNew"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"graphify_affected","arguments":{"node":"authenticateNew"}}}
`
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		cmd := exec.CommandContext(ctx, "docker", "exec", "-i", "--user", "node", container, "adc", "graphify-mcp", project)
		cmd.Stdin = strings.NewReader(input)
		var diagnostics bytes.Buffer
		cmd.Stderr = &diagnostics
		output, err := cmd.Output()
		cancel()
		if err != nil {
			t.Fatalf("MCP: %v %s", err, diagnostics.String())
		}
		decoder := json.NewDecoder(bytes.NewReader(output))
		count := 0
		for decoder.More() {
			var response struct {
				Result struct {
					IsError bool `json:"isError"`
				}
				Error json.RawMessage
			}
			if err := decoder.Decode(&response); err != nil {
				t.Fatalf("MCP contaminated stdout: %v %s", err, output)
			}
			if response.Result.IsError || len(response.Error) > 0 {
				t.Fatalf("MCP tool failed: %s %s", output, diagnostics.String())
			}
			count++
		}
		if count != 6 {
			t.Fatal("incomplete MCP exchange")
		}
		assertContains(string(output), "authenticateNew")
		assertContains(read(filepath.Join(project, "graphify-out", "graph.json")), "authenticateNew")
		// The native worktree still has the earlier source, with its own index.
		run(root, `{"capabilities":["graphify"]}`, "docker", "exec", "-i", "--user", "node", container, "sh", "-c", "cat > '"+filepath.Join(worktree, ".agents", "project.json")+"'")
		docker("exec", "--user", "node", "--env", "ADC_INSTALL_AI_TOOLS=1", container, "adc", "sync", worktree)
		wtGraph := execNode("cat", filepath.Join(worktree, "graphify-out", "graph.json"))
		if strings.Contains(wtGraph, "authenticateNew") {
			t.Fatal("main checkout index leaked into worktree")
		}
		assertContains(wtGraph, "authenticate")
		assertContains(docker("exec", "--user", "node", "--workdir", project, container, "adc", "tools", "graphify", "label-paths"), "graph.html")
		// No credentials or provider/model requests. Only a local dashboard GET.
		startGateway := func() { docker("exec", "-d", "--user", "node", container, "adc", "omniroute", "serve", "--no-open") }
		dashboard := func() {
			t.Helper()
			client := http.Client{Timeout: 3 * time.Second}
			for attempt := 0; attempt < 120; attempt++ {
				response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", omniPort))
				if err == nil {
					body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
					response.Body.Close()
					if response.StatusCode == 200 && strings.Contains(strings.ToLower(string(body)), "omniroute") {
						return
					}
				}
				time.Sleep(time.Second)
			}
			t.Fatal("OmniRoute dashboard did not become reachable")
		}
		startGateway()
		dashboard()
		gatewayKey := sha256.Sum256([]byte(execNode("cat", "/home/node/.omniroute/.env")))
		assertContains(execNode("/opt/ai-tools/bin/omniroute", "--version"), "3.8.51")
		verifyBundles = func() {
			t.Helper()
			if sha256.Sum256([]byte(execNode("cat", "/home/node/.omniroute/.env"))) != gatewayKey {
				t.Fatal("gateway encryption state lost after recreation")
			}
			execNode("test", "-f", filepath.Join(worktree, "graphify-out", "graph.json"))
			assertContains(read(filepath.Join(project, "graphify-out", "graph.json")), "authenticateNew")
			startGateway()
			dashboard()
		}
	}
	t.Log("recreating the container and checking persisted worktrees and home")
	docker("rm", "-f", container)
	adc("workspace", "start")
	execNode("test", "-f", filepath.Join(worktree, "AGENTS.platform.md"))
	assertContains(execNode("cat", "/home/node/adc-smoke-persist"), "ADC_PERSIST_SMOKE")
	assertContains(execNode("cat", "/home/node/.zshrc"), "ADC_USER_SHELL_SETTINGS")
	assertContains(execNode("cat", "/home/node/.history/.bash_history"), "ADC_HISTORY_PERSIST")
	if verifyBundles != nil {
		verifyBundles()
	}
	decode(t, adc("orca", "worktree", "list", "--json"), &trees)
	for _, tree := range trees.Result.Worktrees {
		if tree.Path == worktree {
			return
		}
	}
	t.Fatal("worktree was lost after container recreation")
}

func decode(t *testing.T, data string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), target); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
}
