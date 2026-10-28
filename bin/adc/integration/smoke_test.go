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

	"github.com/foxford/ai-devcontainer/bin/adc/host"
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
	removeContainer := func() {
		t.Helper()
		// Give Orca a normal shutdown before checking persisted application state.
		adc("workspace", "stop")
		docker("rm", container)
	}
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
		// An interrupted handoff deliberately retains its previous container.
		// Remove only this fixture's backups before removing its named volumes.
		if data, err := exec.Command("docker", "container", "ls", "--all", "--filter", "name=^/"+container+"-previous-", "--format", "{{.ID}}").Output(); err == nil {
			if ids := strings.Fields(string(data)); len(ids) > 0 {
				_ = exec.Command("docker", append([]string{"rm", "-f"}, ids...)...).Run()
			}
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
	fixtureBinary := filepath.Join(tmp, "runtime-fixture")
	fixtureBuild := exec.Command("go", "test", "-c", "-o", fixtureBinary, "./bin/adc/integration")
	fixtureBuild.Dir = root
	fixtureBuild.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := fixtureBuild.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v %s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	allocatedPorts := map[int]bool{port: true}
	freePort := func() int {
		t.Helper()
		for {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			if !allocatedPorts[port] {
				allocatedPorts[port] = true
				return port
			}
		}
	}
	omniPort, graphPort := freePort(), freePort()
	adc("workspace", "init", name, "--port", strconv.Itoa(port), "--omniroute-port", strconv.Itoa(omniPort), "--graph-port", strconv.Itoa(graphPort))
	const publishedContainerPort = 27792
	publishedHostPort := freePort()
	adc("workspace", "config", "--publish", fmt.Sprintf("%d:%d", publishedHostPort, publishedContainerPort))
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
		// Dynamic route names are valid Linux Git paths and must not prevent indexing.
		if err := os.MkdirAll(filepath.Join(project, "pages", ":events"), 0755); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(project, "pages", ":events", "api.js"), "export function eventRoute() { return 42; }\n")
		write(filepath.Join(project, "orca.yaml"), "# team setup\nscripts:\n  setup: printf 'ADC_TEAM_SETUP_OK\\n'\n")
		if err := os.Mkdir(filepath.Join(project, ".agents"), 0755); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(project, ".agents", "motd.sh"), "printf 'ADC_PROJECT_BANNER_OK\\n'\n")
		if err := os.MkdirAll(filepath.Join(project, ".agents", "skills"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/opt/dev-platform/docs/skills-README.md", filepath.Join(project, ".agents", "skills", "README.md")); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(project, ".claude", "skills", "mermaid-diagrams"), 0755); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(project, ".claude", "skills", "mermaid-diagrams", "SKILL.md"), "ADC_USER_SKILL_PRESERVED\n")
		run(project, "", "git", "add", "AGENTS.md", "orca.yaml", ".agents/motd.sh", "example.ts", "pages")
		run(project, "", "git", "-c", "user.name=ADC-Smoke", "-c", "user.email=adc-smoke@example.invalid", "commit", "-qm", "fixture")
		args := []string{"project", "add", project, "--id", id}
		if id == "app-a" {
			args = append(args, "--capability", "changesets")
		}
		adc(args...)
	}
	t.Log("building and starting the real runtime")
	adc("ensure-image")
	// Import creates a non-empty volume with volume-nocopy before first start.
	docker("run", "--rm", "--network", "none", "--mount", "type=volume,src="+container+"-home,dst=/home/node,volume-nocopy", "--entrypoint", "sh", "adc-runtime:local", "-c", `mkdir -p /home/node/.codex; printf 'ADC_IMPORTED_AUTH\n' > /home/node/.codex/auth.json; printf '# Installer settings must survive\nexport PATH="$HOME/.local/bin:$PATH"\n# adc:agents:start\n. /opt/ai-devcontainer/runtime/shell-env.sh\n# adc:agents:end\n' > /home/node/.zshrc`)
	startup := adc("workspace", "start")
	assertContains(startup, "Ctrl+C прекращает только ожидание")
	assertContains(startup, "консоль свободна")
	assertContains(adc("workspace", "status"), "running (готов)")
	checkPublishedHTTPPort(t, docker, container, fixtureBinary, publishedHostPort, publishedContainerPort)
	if strings.Contains(docker("info", "--format", "{{json .SecurityOptions}}"), "name=apparmor") {
		assertContains(docker("inspect", "--format", "{{.AppArmorProfile}}", container), "adc-runtime-")
	}
	assertContains(execNode("sh", "-c", "command -v bwrap && bwrap --version"), "bubblewrap")
	// A default Docker profile denies the user namespace required by Codex bwrap.
	execNode("unshare", "-Ur", "true")
	for _, id := range []string{"app-a", "app-b"} {
		assertContains(read(filepath.Join(tmp, id, ".claude", "skills", "mermaid-diagrams", "SKILL.md")), "ADC_USER_SKILL_PRESERVED")
		execNode("test", "-f", filepath.Join(tmp, id, ".agents", "skills", "README.md"))
	}
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
	assertContains(execNode("cat", "/home/node/.codex/auth.json"), "ADC_IMPORTED_AUTH")
	zsh := docker("exec", "-t", "--user", "node", "--workdir", filepath.Join(tmp, "app-a"), container, "zsh", "-ic", `(( $+functions[omz] && $+functions[p10k] )) || exit 1; [[ "$ZSH_THEME" == powerlevel10k/powerlevel10k ]] || exit 1; print -r -- "ADC_PROMPT=$PS1"`)
	assertContains(zsh, "ADC · "+name)
	assertContains(zsh, "ADC_PROJECT_BANNER_OK")
	assertContains(zsh, "["+name+"]")
	assertContains(execNode("cat", "/home/node/.zshrc"), "# Installer settings must survive")
	// A later user-defined prompt takes precedence over the ADC defaults.
	execNode("sh", "-c", `printf 'PROMPT="ADC_PERSONAL_PROMPT "\n' > "$HOME/.zshrc"`)
	removeContainer()
	adc("workspace", "start")
	checkPublishedHTTPPort(t, docker, container, fixtureBinary, publishedHostPort, publishedContainerPort)
	custom := execNode("zsh", "-ic", `(( ! $+functions[omz] )) || exit 1; print -r -- "$PS1"`)
	assertContains(custom, "ADC_PERSONAL_PROMPT")
	// Restore the vendor-only profile and rerun initialization for the rest of the smoke.
	execNode("sh", "-c", `printf '# Installer settings must survive\nexport PATH="$HOME/.local/bin:$PATH"\n' > "$HOME/.zshrc"`)
	previousContainer := strings.TrimSpace(docker("inspect", "--format", "{{.Id}}", container))
	publishedHostPort = freePort()
	adc("workspace", "config", "--publish", fmt.Sprintf("127.0.0.1:%d:%d", publishedHostPort, publishedContainerPort))
	adc("workspace", "start")
	if current := strings.TrimSpace(docker("inspect", "--format", "{{.Id}}", container)); current == previousContainer {
		t.Fatal("changed published port did not recreate the container")
	}
	checkPublishedHTTPPort(t, docker, container, fixtureBinary, publishedHostPort, publishedContainerPort)
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
	assertContains(adc("project", "relate", "app-b", "app-a"), "Связь сохранена: app-b → app-a")
	assertContains(adc("workspace", "map"), "Карта проектов и знаний обновлена")
	assertContains(execNode("cat", "/workspace/AGENTS.workspace.md"), "Связи: app-a")
	// Existing registrations can change team capabilities from either side;
	// the command applies delivery immediately and leaves the neighbor intact.
	projectB := filepath.Join(tmp, "app-b")
	run(projectB, "", cli, "project", "capability", "add", "changesets")
	assertContains(read(filepath.Join(projectB, "AGENTS.platform.md")), "Changesets")
	catalogOutput := run(projectB, "", cli, "project", "capability", "list")
	assertContains(catalogOutput, "✓")
	assertContains(catalogOutput, "graphify")
	docker("exec", "--user", "node", "--workdir", projectB, container, "adc", "project", "capability", "add", "pnpm-nx")
	execNode("test", "-f", filepath.Join(projectB, "MONOREPO.md"))
	docker("exec", "--user", "node", "--workdir", projectB, container, "adc", "project", "capability", "rm", "changesets", "pnpm-nx")
	if strings.Contains(read(filepath.Join(projectB, "AGENTS.platform.md")), "Changesets") {
		t.Fatal("removed team capability remained in instructions")
	}
	assertContains(read(filepath.Join(tmp, "app-a", "AGENTS.platform.md")), "Changesets")
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
	t.Log("checking that a project delivery error keeps Orca running")
	contract := filepath.Join(tmp, "app-a", "AGENTS.platform.md")
	originalContract := read(contract)
	write(contract, "ADC_UNMANAGED_PROJECT_RULES\n")
	removeContainer()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	start := exec.CommandContext(ctx, cli, "workspace", "start")
	start.Dir = root
	failedOutput, startErr := start.CombinedOutput()
	cancel()
	if startErr == nil {
		t.Fatal("project delivery error was hidden")
	}
	assertContains(string(failedOutput), "app-a")
	assertContains(string(failedOutput), "AGENTS.platform.md принадлежит проекту")
	assertContains(adc("workspace", "status"), "ошибка подготовки")
	decode(t, adc("orca", "status", "--json"), &status)
	if !status.Result.Runtime.Reachable {
		t.Fatal("project error stopped Orca")
	}
	if strings.TrimSpace(docker("inspect", "--format", "{{.State.Status}} {{.RestartCount}}", container)) != "running 0" {
		t.Fatal("project failure restarted the runtime")
	}
	if read(contract) != "ADC_UNMANAGED_PROJECT_RULES\n" {
		t.Fatal("unmanaged project contract overwritten")
	}
	write(contract, originalContract)
	adc("workspace", "stop")
	adc("workspace", "start")
	assertContains(adc("workspace", "status"), "running (готов)")
	decode(t, adc("orca", "worktree", "list", "--json"), &trees)
	persisted := false
	for _, tree := range trees.Result.Worktrees {
		if tree.Path == worktree {
			persisted = true
		}
	}
	if !persisted {
		t.Fatalf("worktree %s was lost after project delivery error and restart: %+v", worktree, trees.Result.Worktrees)
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

	t.Log("checking capability activation and authenticated MCP doctor inside the runtime")
	docker("cp", fixtureBinary, container+":/tmp/adc-mcp-fixture")
	docker("exec", container, "chmod", "755", "/tmp/adc-mcp-fixture")
	docker("exec", "-d", "--user", "node", "--env", "ADC_MCP_HTTP_FIXTURE=1", container, "/tmp/adc-mcp-fixture", "-test.run=^TestDockerMCPHTTPFixture$")
	// The shell only launches a Go test fixture; config generation remains Go.
	run(root, "#!/bin/sh\nADC_CODEX_CONFIG_FIXTURE=1 exec /tmp/adc-mcp-fixture -test.run=^TestDockerCodexConfigFixture$ -- \"$@\"\n", "docker", "exec", "-i", "--user", "node", container, "sh", "-c", "cat > /opt/ai-tools/bin/codex; chmod +x /opt/ai-tools/bin/codex")
	projectA := filepath.Join(tmp, "app-a")
	write(filepath.Join(projectA, ".agents", "mcp.local.json"), `{"mcpServers":{"obsidian":{"url":"http://127.0.0.1:27791/mcp/"}}}`)
	write(filepath.Join(projectA, ".agents", "mcp.secrets.env"), "OBSIDIAN_API_KEY=adc-fixture-key\n")
	assertContains(run(projectA, "", cli, "project", "capability", "add", "obsidian"), "tools/list OK")
	assertContains(run(projectA, "", cli, "mcp", "doctor", "obsidian"), "инструментов: 1")
	// Native formats contain headers; human preferences and another checkout survive.
	codexConfig := execNode("cat", "/home/node/.codex/config.toml")
	assertContains(codexConfig, "http_headers")
	assertContains(codexConfig, "Bearer adc-fixture-key")
	assertContains(codexConfig, "enabled = false")
	codexScope := read(filepath.Join(projectA, ".codex", "config.toml"))
	assertContains(codexScope, "enabled = true")
	assertContains(codexScope, "-obsidian")
	if strings.Contains(codexScope, "adc-fixture-key") || strings.Contains(codexScope, "app-b-") {
		t.Fatal("Codex project scope contains a secret or another project's MCP")
	}
	assertContains(read(filepath.Join(projectA, ".adc", "hermes-managed", "config.yaml")), "Bearer adc-fixture-key")
	assertContains(read(filepath.Join(projectA, ".adc", "dsh-mcp.patch.yml")), "Bearer adc-fixture-key")
	for _, path := range []string{"/home/node/.hermes/config.yaml", "/home/node/.dsh/cordis.patch.yml"} {
		if strings.Contains(execNode("cat", path), "adc-fixture-key") {
			t.Fatal("project MCP leaked into a shared agent config")
		}
	}
	assertContains(read(filepath.Join(projectA, ".adc", "opencode.json")), "\"remote\"")
	assertContains(read(filepath.Join(projectA, ".mcp.json")), "Bearer adc-fixture-key")
	run(root, "#!/bin/sh\nprintf '%s' \"$OPENCODE_CONFIG_CONTENT\" > /tmp/adc-opencode-env.json\nprintf 'ADC_OPENCODE_STARTED\\n'\n", "docker", "exec", "-i", "--user", "node", container, "sh", "-c", "cat > /opt/ai-tools/bin/opencode; chmod +x /opt/ai-tools/bin/opencode")
	assertContains(docker("exec", "--user", "node", "--workdir", projectA, container, "opencode"), "ADC_OPENCODE_STARTED")
	assertContains(execNode("cat", "/tmp/adc-opencode-env.json"), "Bearer adc-fixture-key")
	foreign := `{"mcpServers":{"personal":{"command":"personal-cli"}}}`
	// Simulate a previously human-owned Claude file in a separate checkout.
	write(filepath.Join(projectB, ".mcp.json"), foreign)
	// B was originally ADC-owned: preserve the existing ownership contract.
	write(filepath.Join(projectB, ".adc", "mcp-state.json"), `{"version":1,"servers":[],"claudeOwned":false}`)
	run(root, "#!/bin/sh\nprintf '%s\\n' \"$@\" > /tmp/adc-claude-args.txt\nprintf 'ADC_CLAUDE_STARTED\\n'\n", "docker", "exec", "-i", "--user", "node", container, "sh", "-c", "cat > /opt/ai-tools/bin/claude; chmod +x /opt/ai-tools/bin/claude")
	assertContains(docker("exec", "--user", "node", "--workdir", projectB, container, "claude"), "ADC_CLAUDE_STARTED")
	assertContains(execNode("cat", "/tmp/adc-claude-args.txt"), "--mcp-config")
	if read(filepath.Join(projectB, ".mcp.json")) != foreign {
		t.Fatal("human Claude MCP config changed")
	}
	write(filepath.Join(projectA, ".agents", "mcp.secrets.env"), "OBSIDIAN_API_KEY=wrong-fixture-key\n")
	failedDoctor := exec.Command(cli, "mcp", "doctor", "obsidian")
	failedDoctor.Dir = projectA
	output, doctorErr := failedDoctor.CombinedOutput()
	if doctorErr == nil || !strings.Contains(string(output), "401/403") || strings.Contains(string(output), "wrong-fixture-key") {
		t.Fatalf("bad API key passed doctor or leaked: %v %s", doctorErr, strings.ReplaceAll(string(output), "wrong-fixture-key", "[redacted]"))
	}
	write(filepath.Join(projectA, ".agents", "mcp.secrets.env"), "OBSIDIAN_API_KEY=adc-fixture-key\n")
	assertContains(run(projectA, "", cli, "project", "capability", "rm", "obsidian"), "app-a OK")
	// Restore the pre-existing smoke agent; following bundle tests use this stub.
	run(root, "#!/bin/sh\nprintf 'ADC_AGENT_STARTED\\n'\n", "docker", "exec", "-i", "--user", "node", container, "sh", "-c", "cat > /opt/ai-tools/bin/codex; chmod +x /opt/ai-tools/bin/codex")

	var verifyBundles func()
	if os.Getenv("ADC_TOOL_BUNDLES_INTEGRATION") == "1" {
		t.Log("checking native Codex sandbox: project reads/writes and denied outside write")
		// Install the already-supported agent in an isolated prefix, without real
		// accounts/config or model requests. Exercise bwrap, not a mocked CLI.
		codexPrefix := "/tmp/adc-smoke-native-codex"
		arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
		if arch == "" {
			t.Fatalf("unsupported Codex sandbox test architecture: %s", runtime.GOARCH)
		}
		// npm may silently omit an optional native package; make the platform
		// artifact explicit so an installation failure is reported here.
		native := "@openai/codex-linux-" + arch + "@npm:@openai/codex@0.162.1-linux-" + arch
		execNode("npm", "install", "--prefix", codexPrefix, "--no-audit", "--no-fund", "@openai/codex@0.162.1", native)
		codexHome := "/home/node/.cache/adc-smoke-native-codex-home"
		execNode("mkdir", "-p", codexHome)
		outside := "/home/node/adc-smoke-sandbox-outside"
		execNode("sh", "-c", "printf 'ADC_UNCHANGED\\n' > "+outside)
		script := `cat /workspace/AGENTS.md /workspace/AGENTS.workspace.md AGENTS.platform.md AGENTS.skills.md >/dev/null
printf 'ADC_ALLOWED\n' > adc-smoke-sandbox-inside
if (printf 'ADC_FORBIDDEN\n' > /home/node/adc-smoke-sandbox-outside) 2>/dev/null; then
  echo ADC_SANDBOX_OUTSIDE_WRITE_ALLOWED
  exit 1
fi
echo ADC_SANDBOX_READ_WRITE_OK
`
		result := execNode("env", "CODEX_HOME="+codexHome, filepath.Join(codexPrefix, "node_modules", ".bin", "codex"), "sandbox", "-P", ":workspace", "-C", filepath.Join(tmp, "app-a"), "--", "/bin/sh", "-ec", script)
		assertContains(result, "ADC_SANDBOX_READ_WRITE_OK")
		if strings.Contains(result, "could not find bubblewrap") || strings.Contains(result, "Refusing to create helper binaries") {
			t.Fatalf("native Codex sandbox still warns about prerequisites: %s", result)
		}
		assertContains(execNode("cat", outside), "ADC_UNCHANGED")
		assertContains(read(filepath.Join(tmp, "app-a", "adc-smoke-sandbox-inside")), "ADC_ALLOWED")
		execNode("rm", "-f", outside, filepath.Join(tmp, "app-a", "adc-smoke-sandbox-inside"))
		t.Log("installing real Graphify and OmniRoute without agent accounts")
		execNode("adc", "tools", "install", "graphify")
		execNode("adc", "tools", "install", "omniroute")
		project := filepath.Join(tmp, "app-a")
		write(filepath.Join(project, ".agents", "project.json"), `{"capabilities":["graphify"]}`)
		checkBackgroundGraphify(t, root, tmp, cli, container, project)
		// Reproduce a native MCP client's allowlisted environment: use only
		// HOME/PATH plus the environment explicitly delivered with this server.
		var delivered struct {
			Servers map[string]struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(read(filepath.Join(project, ".mcp.json"))), &delivered); err != nil {
			t.Fatal(err)
		}
		checked := false
		for name, server := range delivered.Servers {
			if !strings.HasSuffix(name, "-graphify") {
				continue
			}
			args := []string{"exec", "-i", "--user", "node", container, "env", "-i", "HOME=/home/node", "PATH=/usr/local/bin:/usr/bin:/bin:/opt/ai-tools/bin"}
			for key, value := range server.Env {
				args = append(args, key+"="+value)
			}
			args = append(args, server.Command)
			args = append(args, server.Args...)
			response := run(root, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-03-26\"}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n", "docker", args...)
			assertContains(response, "adc-graphify")
			assertContains(response, "graphify_affected")
			checked = true
		}
		if !checked {
			t.Fatal("Graphify missing from delivered MCP configuration")
		}
		assertContains(read(filepath.Join(project, "graphify-out", "graph.json")), "authenticate")
		assertContains(read(filepath.Join(project, "graphify-out", "graph.json")), "pages/:events/api.js")
		if _, err := os.Stat(filepath.Join(tmp, "app-b", "graphify-out")); !os.IsNotExist(err) {
			t.Fatal("graph leaked into unrelated project")
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
		wtGraph := waitForGraphify(t, container, worktree, "authenticate")
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
	t.Log("applying an update to a running workspace and waiting for readiness")
	updated := &host.App{Platform: root, State: os.Getenv("ADC_HOME")}
	before := docker("inspect", "--format", "{{.State.StartedAt}}", container)
	// Exercise the new helper's image build and real Docker lifecycle; installing
	// a desktop client or changing the developer's host shell is out of scope.
	if err := updated.ApplyUpdate(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if after := docker("inspect", "--format", "{{.State.StartedAt}}", container); after == before {
		t.Fatal("update did not restart the running workspace")
	}
	assertContains(adc("workspace", "status"), "running (готов)")
	execNode("test", "-f", filepath.Join(worktree, "AGENTS.platform.md"))
	assertContains(execNode("cat", "/home/node/adc-smoke-persist"), "ADC_PERSIST_SMOKE")
	if _, err := os.Stat(filepath.Join(os.Getenv("ADC_HOME"), "workspace-replacement.json")); !os.IsNotExist(err) {
		t.Fatal("successful update retained a replacement journal", err)
	}
	t.Run("update failure and interrupted replacement", func(t *testing.T) {
		proxy, realDocker := dockerUpdateFailureProxy(t, tmp)
		marker := filepath.Join(tmp, "docker-failure-mode")
		t.Setenv("ADC_SMOKE_REAL_DOCKER", realDocker)
		t.Setenv("ADC_SMOKE_DOCKER_FAILURE", marker)
		t.Setenv("PATH", proxy+string(os.PathListSeparator)+os.Getenv("PATH"))
		previous := strings.TrimSpace(docker("inspect", "--format", "{{.Id}}", container))
		write(marker, "run")
		if err := updated.ApplyUpdate(func() error { return nil }); err == nil || !strings.Contains(err.Error(), "прежний контейнер восстановлен") {
			t.Fatalf("creation failure did not restore previous runtime: %v", err)
		}
		if current := strings.TrimSpace(docker("inspect", "--format", "{{.Id}}", container)); current != previous {
			t.Fatal("restoration recreated rather than recovered previous container")
		}
		adc("workspace", "start")
		assertContains(adc("workspace", "status"), "running (готов)")
		assertContains(execNode("cat", "/home/node/adc-smoke-persist"), "ADC_PERSIST_SMOKE")
		// Force replacement without changing the developer's source checkout.
		write(filepath.Join(os.Getenv("ADC_HOME"), "container-key"), "ADC_SMOKE_STALE_KEY")
		write(marker, "rename")
		cmd := exec.Command(cli, "workspace", "start")
		if data, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(data), "ADC_SMOKE_LOST_RENAME_RESPONSE") {
			t.Fatalf("did not interrupt container handoff: %v %s", err, data)
		}
		journal := filepath.Join(os.Getenv("ADC_HOME"), "workspace-replacement.json")
		if _, err := os.Stat(journal); err != nil {
			t.Fatal("interrupted handoff lost its journal", err)
		}
		adc("workspace", "start")
		assertContains(adc("workspace", "status"), "running (готов)")
		assertContains(execNode("cat", "/home/node/adc-smoke-persist"), "ADC_PERSIST_SMOKE")
		if _, err := os.Stat(journal); !os.IsNotExist(err) {
			t.Fatal("resumed handoff retained its journal", err)
		}
		if backups := docker("container", "ls", "--all", "--filter", "name=^/"+container+"-previous-", "--format", "{{.Names}}"); strings.TrimSpace(backups) != "" {
			t.Fatalf("previous containers leaked: %s", backups)
		}
	})
	adc("workspace", "stop")
	if err := updated.ApplyUpdate(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	assertContains(adc("workspace", "status"), "exited")
	adc("workspace", "start")
	t.Log("clearing published ports and checking persisted worktrees and home after recreation")
	previousContainer = strings.TrimSpace(docker("inspect", "--format", "{{.Id}}", container))
	adc("workspace", "config", "--publish-clear")
	adc("workspace", "start")
	if current := strings.TrimSpace(docker("inspect", "--format", "{{.Id}}", container)); current == previousContainer {
		t.Fatal("clearing published ports did not recreate the container")
	}
	checkPublishedPortBindings(t, docker, container, 0, publishedContainerPort)
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
