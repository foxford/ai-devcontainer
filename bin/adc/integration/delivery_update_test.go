package integration

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/host"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

// This exercises real host/runtime commands against a local Git upstream with
// networking disabled in the running container. The ordinary workspace smoke
// covers Orca startup; here a persistent process makes any restart observable.
func TestDockerDeliveryUpdate(t *testing.T) {
	if os.Getenv("ADC_INTEGRATION") != "1" {
		t.Skip("set ADC_INTEGRATION=1 to test delivery in the real Docker runtime")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Docker delivery integration runs on Linux")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("Docker is required for the explicitly requested integration test:", err)
	}
	root, tmp := testutil.Root(t), t.TempDir()
	name := fmt.Sprintf("delivery-%d-%d", os.Getpid(), time.Now().UnixNano())
	container := "adc-" + name
	state, upstream, platform := filepath.Join(tmp, "state"), filepath.Join(tmp, "upstream"), filepath.Join(tmp, "platform")
	cli := filepath.Join(tmp, "adc")
	t.Setenv("ADC_RUNTIME", "0")
	t.Setenv("ADC_HOME", state)
	t.Setenv("AI_DEVCONTAINER_HOME", platform)
	t.Setenv("ADC_INSTALL_AI_TOOLS", "0")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	run := func(dir, binary string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = dir
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %q: %v\n%s", binary, args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	docker := func(args ...string) string { t.Helper(); return run(root, "docker", args...) }
	execNode := func(args ...string) string {
		t.Helper()
		return docker(append([]string{"exec", "--user", "node", "--env", "HOME=/home/node", container}, args...)...)
	}
	write := func(path, value string) {
		t.Helper()
		if err := shared.AtomicWrite(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(dir string) {
		t.Helper()
		run(dir, "git", "add", ".")
		run(dir, "git", "-c", "user.name=ADC", "-c", "user.email=adc@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	}
	initialize := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		run(dir, "git", "init", "-q", "-b", "main")
	}
	assertContains := func(value, expected string) {
		t.Helper()
		if !strings.Contains(value, expected) {
			t.Fatalf("expected %q in:\n%s", expected, value)
		}
	}

	initialize(upstream)
	write(filepath.Join(upstream, "delivery/catalog.json"), `{"version":1,"base":{"instructions":["instructions/AGENTS.platform.md"],"skills":"skills","mcp":{"source":"mcp.json","servers":[]},"documents":[{"source":"documents/linked.md","target":"GUIDE-LINK.md","mode":"link"},{"source":"documents/copied.md","target":"GUIDE-COPY.md","mode":"copy"},{"source":"documents/copied.md","target":"USER-GUIDE.md","mode":"copy"}]},"capabilities":{}}`)
	write(filepath.Join(upstream, "delivery/mcp.json"), `{"mcpServers":{}}`)
	publish := func(version string) {
		t.Helper()
		write(filepath.Join(upstream, "delivery/instructions/AGENTS.platform.md"), "Contract "+version+"\n")
		write(filepath.Join(upstream, "delivery/instructions/AGENTS.platform.md.meta.json"), fmt.Sprintf(`{"kind":"document","name":"platform-rules","version":%q}`, version))
		for _, document := range []string{"linked", "copied"} {
			write(filepath.Join(upstream, "delivery/documents", document+".md"), "Document "+version+"\n")
			write(filepath.Join(upstream, "delivery/documents", document+".md.meta.json"), fmt.Sprintf(`{"kind":"document","name":%q,"version":%q}`, document, version))
		}
		for _, skill := range []string{"demo", "forked"} {
			write(filepath.Join(upstream, "delivery/skills", skill, "SKILL.md"), "---\nname: "+skill+"\ndescription: Integration fixture.\n---\n\nStock "+version+"\n")
			write(filepath.Join(upstream, "delivery/skills", skill, "meta.json"), fmt.Sprintf(`{"kind":"skill","name":%q,"version":%q}`, skill, version))
		}
		// Delivery updates must not install files outside delivery/.
		write(filepath.Join(upstream, "platform-code.txt"), "Platform "+version+"\n")
		commit(upstream)
	}
	publish("1.0.0")
	run(tmp, "git", "clone", "-q", upstream, platform)
	baseRevision := run(platform, "git", "rev-parse", "HEAD")
	config := shared.Config{Version: shared.Schema, Name: name, Port: 6768}
	for _, id := range []string{"app-a", "app-b", "readonly"} {
		path := filepath.Join(tmp, id)
		initialize(path)
		write(filepath.Join(path, "AGENTS.md"), "Keep project rules.\n")
		write(filepath.Join(path, "USER-GUIDE.md"), "Keep manual documentation.\n")
		if id == "app-a" {
			write(filepath.Join(path, ".agents/skills/forked/SKILL.md"), "---\nname: forked\ndescription: Project fork.\n---\n\nKeep project fork.\n")
		}
		if id == "readonly" {
			write(filepath.Join(path, "AGENTS.platform.md"), "Keep read-only contract.\n")
		}
		commit(path)
		config.Projects = append(config.Projects, shared.Project{ID: id, Path: path, RuntimePath: path, ReadOnly: id == "readonly"})
	}
	if err := (&shared.App{State: state}).Save(config); err != nil {
		t.Fatal(err)
	}
	readonlyDigest, err := shared.TreeDigest(config.Projects[2].Path)
	if err != nil {
		t.Fatal(err)
	}
	run(root, "go", "build", "-o", cli, "./bin/adc")
	hostBinary, err := os.ReadFile(cli)
	if err != nil {
		t.Fatal(err)
	}
	hostHash := sha256.Sum256(hostBinary)
	// Build once from the real sources. The fixture platform intentionally has
	// no Dockerfile or Go sources, so an accidental full update cannot succeed.
	if err := (&host.App{Platform: root, State: state}).EnsureImage(); err != nil {
		t.Fatal(err)
	}
	image := docker("image", "inspect", "--format", "{{.Id}}", "adc-runtime:local")
	t.Cleanup(func() {
		if t.Failed() {
			output, _ := exec.Command("docker", "logs", "--tail", "100", container).CombinedOutput()
			t.Logf("delivery container logs:\n%s", output)
		}
		for _, args := range [][]string{{"rm", "-f", container}, {"volume", "rm", container + "-home"}} {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
			cancel()
			if err != nil {
				t.Logf("cleanup: %v: %s", err, output)
			}
		}
	})
	args := []string{"run", "-d", "--name", container, "--network", "none", "--entrypoint", "sleep", "-e", "ADC_RUNTIME=1", "-e", "ADC_INSTALL_AI_TOOLS=0", "-e", "ADC_HOME=/var/lib/adc", "-e", "AI_DEVCONTAINER_HOME=/opt/ai-devcontainer", "-e", "HOME=/home/node", "--mount", "type=volume,src=" + container + "-home,dst=/home/node"}
	for _, mount := range []string{"type=bind,src=" + state + ",dst=/var/lib/adc", "type=bind,src=" + platform + ",dst=/opt/ai-devcontainer,readonly", "type=bind,src=" + upstream + ",dst=" + upstream + ",readonly"} {
		args = append(args, "--mount", mount)
	}
	for _, project := range config.Projects {
		mount := "type=bind,src=" + project.Path + ",dst=" + project.RuntimePath
		if project.ReadOnly {
			mount += ",readonly"
		}
		args = append(args, "--mount", mount)
	}
	docker(append(args, image, "infinity")...)
	uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
	docker("exec", container, "groupmod", "--non-unique", "--gid", gid, "node")
	docker("exec", container, "usermod", "--non-unique", "--uid", uid, "--gid", gid, "node")
	docker("exec", container, "mkdir", "-p", "/workspace/projects")
	docker("exec", container, "chown", "-R", uid+":"+gid, "/home/node", "/workspace")
	worktree := "/home/node/delivery-worktree"
	execNode("git", "-C", config.Projects[0].RuntimePath, "worktree", "add", "--detach", "-q", worktree, "HEAD")
	execNode("adc", "sync", "--all")
	identity := docker("inspect", "--format", "{{.Id}} {{.Image}} {{.State.StartedAt}} {{.RestartCount}}", container)
	runtimeHash := execNode("sha256sum", "/usr/local/bin/adc")
	runtimeVersion := execNode("adc", "--version")

	verify := func(version string) {
		t.Helper()
		for _, path := range []string{config.Projects[0].RuntimePath, config.Projects[1].RuntimePath, worktree} {
			assertContains(execNode("cat", filepath.Join(path, "AGENTS.platform.md")), "Contract "+version)
			assertContains(execNode("cat", filepath.Join(path, ".claude/skills/demo/SKILL.md")), "Stock "+version)
			assertContains(execNode("cat", filepath.Join(path, "AGENTS.md")), "Keep project rules.")
			for _, name := range []string{"GUIDE-LINK.md", "GUIDE-COPY.md"} {
				assertContains(execNode("cat", filepath.Join(path, name)), "Document "+version)
			}
			execNode("test", "-L", filepath.Join(path, "GUIDE-LINK.md"))
			execNode("test", "!", "-L", filepath.Join(path, "GUIDE-COPY.md"))
			if body := execNode("cat", filepath.Join(path, "USER-GUIDE.md")); body != "Keep manual documentation." {
				t.Fatalf("manual document changed in %s: %s", path, body)
			}
			if path != config.Projects[1].RuntimePath {
				assertContains(execNode("cat", filepath.Join(path, ".claude/skills/forked/SKILL.md")), "Keep project fork.")
			}
		}
		assertContains(execNode("cat", "/home/node/.codex/skills/demo/SKILL.md"), "Stock "+version)
		assertContains(execNode("cat", "/workspace/AGENTS.md"), "Contract "+version)
		if after, err := shared.TreeDigest(config.Projects[2].Path); err != nil || after != readonlyDigest {
			t.Fatalf("read-only project changed: %v", err)
		}
		if after := docker("inspect", "--format", "{{.Id}} {{.Image}} {{.State.StartedAt}} {{.RestartCount}}", container); after != identity {
			t.Fatalf("container restarted or replaced: before %s; after %s", identity, after)
		}
		if execNode("sha256sum", "/usr/local/bin/adc") != runtimeHash || execNode("adc", "--version") != runtimeVersion {
			t.Fatal("runtime binary changed")
		}
		binary, err := os.ReadFile(cli)
		if err != nil || sha256.Sum256(binary) != hostHash {
			t.Fatalf("host binary changed: %v", err)
		}
		if run(platform, "git", "rev-parse", "HEAD") != baseRevision || execNode("git", "-C", "/opt/ai-devcontainer", "rev-parse", "HEAD") != baseRevision {
			t.Fatal("platform checkout revision changed")
		}
		assertContains(execNode("cat", "/opt/ai-devcontainer/platform-code.txt"), "Platform 1.0.0")
		if docker("image", "inspect", "--format", "{{.Id}}", "adc-runtime:local") != image {
			t.Fatal("runtime image changed")
		}
	}
	verify("1.0.0")
	publish("1.1.0")
	run(root, cli, "update", "--delivery")
	verify("1.1.0")
	publish("1.2.0")
	execNode("adc", "update", "--delivery")
	verify("1.2.0")
}
