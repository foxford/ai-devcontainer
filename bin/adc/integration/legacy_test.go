package integration

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/legacy"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

// Real Docker mounts/exports/imports, but only fake accounts and disposable data.
func TestDockerLegacyMigration(t *testing.T) {
	if os.Getenv("ADC_INTEGRATION") != "1" || goruntime.GOOS != "linux" {
		t.Skip("Linux Docker integration opt-in")
	}
	root, tmp := testutil.Root(t), t.TempDir()
	name := fmt.Sprintf("legacy-%d-%d", os.Getpid(), time.Now().UnixNano())
	oldTools, oldDinD := name+"-old-tools", name+"-old-dind"
	image := "adc-legacy-fixture:" + name
	newContainer := "adc-" + name
	cli := filepath.Join(tmp, "adc")
	t.Setenv("ADC_HOME", filepath.Join(tmp, "state"))
	t.Setenv("AI_DEVCONTAINER_HOME", root)
	t.Setenv("ADC_INSTALL_AI_TOOLS", "0")
	t.Setenv("ADC_RUNTIME", "0")
	execute := func(binary string, args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		data, err := cmd.CombinedOutput()
		return string(data), err
	}
	run := func(binary string, args ...string) string {
		t.Helper()
		output, err := execute(binary, args...)
		if err != nil {
			t.Fatalf("%s %q: %v\n%s", binary, args, err, output)
		}
		return output
	}
	adc := func(args ...string) string { t.Helper(); return run(cli, args...) }
	docker := func(args ...string) string { t.Helper(); return run("docker", args...) }
	write := func(file, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(file string) string {
		t.Helper()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	freePort := func() string {
		t.Helper()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
		if err = l.Close(); err != nil {
			t.Fatal(err)
		}
		return port
	}
	oldContainers := []string{name + "-app-a", name + "-app-b"}
	t.Cleanup(func() {
		for _, container := range append(oldContainers, newContainer) {
			_, _ = execute("docker", "rm", "-f", container)
		}
		for _, volume := range []string{oldTools, oldDinD, newContainer + "-home", newContainer + "-tools", newContainer + "-worktrees"} {
			_, _ = execute("docker", "volume", "rm", volume)
		}
		_, _ = execute("docker", "image", "rm", image)
	})
	run("go", "build", "-o", cli, "./bin/adc")
	adc("ensure-image")
	// A tiny scratch fixture keeps docker export meaningful without copying a
	// multi-gigabyte toolchain twice. Its stores model the old writable layer.
	keepalive := filepath.Join(tmp, "keepalive.go")
	write(keepalive, "package main\nimport \"time\"\nfunc main(){for {time.Sleep(time.Hour)}}\n")
	binary := filepath.Join(tmp, "keepalive")
	run("go", "build", "-o", binary, keepalive)
	var layer bytes.Buffer
	tw := tar.NewWriter(&layer)
	for file, data := range map[string][]byte{
		"keepalive": []byte(read(binary)),
		"home/node/.local/share/opencode/auth.json":   []byte("fake-writable-layer-auth"),
		"home/node/.local/share/opencode/opencode.db": []byte("fake-opencode-database"),
	} {
		mode := int64(0600)
		if file == "keepalive" {
			mode = 0700
		}
		if err := tw.WriteHeader(&tar.Header{Name: file, Size: int64(len(data)), Mode: mode}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	layerFile := filepath.Join(tmp, "layer.tar")
	if err := os.WriteFile(layerFile, layer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	docker("import", "--change", "ENTRYPOINT [\"/keepalive\"]", layerFile, image)
	state := filepath.Join(tmp, ".ai-devcontainer-dev")
	write(filepath.Join(state, "projects"), "app-a\napp-b\ninactive-project\n")
	write(filepath.Join(state, "inactive-project", "codex", "auth.json"), "fake-inactive-account")
	toolsSeed := filepath.Join(tmp, "tools-seed")
	write(filepath.Join(toolsSeed, "share/hermes/auth.json"), "fake-shared-hermes-auth")
	write(filepath.Join(toolsSeed, "share/mcp/servers.json"), "{\"mcpServers\":{}}\n")
	write(filepath.Join(toolsSeed, "share/mcp/secrets.env"), "FAKE_PROVIDER_KEY=fixture\n")
	docker("volume", "create", oldTools)
	docker("volume", "create", oldDinD)
	docker("run", "--rm", "--entrypoint", "cp", "--mount", "type=bind,src="+toolsSeed+",dst=/seed,readonly", "--mount", "type=volume,src="+oldTools+",dst=/destination", "adc-runtime:local", "-a", "/seed/.", "/destination/")
	docker("run", "--rm", "--entrypoint", "sh", "--mount", "type=volume,src="+oldDinD+",dst=/destination", "adc-runtime:local", "-c", "printf 'fake-dind-data' > /destination/persistent.db")
	for i, id := range []string{"app-a", "app-b"} {
		project := filepath.Join(tmp, id)
		write(filepath.Join(project, "AGENTS.md"), "Keep team rules.\n")
		write(filepath.Join(project, "untracked.txt"), id+"-untracked")
		write(filepath.Join(project, ".gitignore"), "ignored.secret\n")
		write(filepath.Join(project, "ignored.secret"), "fake-ignored-secret")
		run("git", "-C", project, "init", "-q", "-b", "main")
		run("git", "-C", project, "add", "AGENTS.md", ".gitignore")
		run("git", "-C", project, "-c", "user.name=ADC-Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
		args := []string{"create", "--name", oldContainers[i], "--label", "devcontainer.local_folder=" + project, "--mount", "type=bind,src=" + project + ",dst=/workspaces/" + id}
		for _, agent := range []string{"codex", "claude", "hermes"} {
			source := filepath.Join(state, id, agent)
			if err := os.MkdirAll(source, 0755); err != nil {
				t.Fatal(err)
			}
			switch agent {
			case "codex":
				write(filepath.Join(source, "auth.json"), id+"-fake-auth")
				write(filepath.Join(source, "sessions", "chat.jsonl"), id+"-session")
				write(filepath.Join(source, "sessions", id+".jsonl"), id+"-unique")
			case "claude":
				write(filepath.Join(source, ".credentials.json"), id+"-fake-credentials")
			case "hermes":
				if err := os.Symlink("/opt/ai-tools/share/hermes/auth.json", filepath.Join(source, "auth.json")); err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(source, "config.yaml"), "model: fake-model\n")
			}
			args = append(args, "--mount", "type=bind,src="+source+",dst=/home/node/."+agent)
		}
		args = append(args, "--mount", "type=volume,src="+oldTools+",dst=/opt/ai-tools", "--mount", "type=volume,src="+oldDinD+",dst=/var/lib/docker", image)
		docker(args...)
	}
	planFile, backupDir := filepath.Join(tmp, "plan.json"), filepath.Join(tmp, "backup")
	adc("migrate", "legacy", "plan", "--container", strings.Join(oldContainers, ","), "--output", planFile)
	docker("start", oldContainers[0])
	output, err := execute(cli, "migrate", "legacy", "backup", planFile, "--output", backupDir)
	if err == nil || !strings.Contains(output, "docker stop") {
		t.Fatalf("allowed live copy: %v %s", err, output)
	}
	if _, err = os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatal("created backup before stopping writers")
	}
	docker("stop", oldContainers[0])
	adc("migrate", "legacy", "backup", planFile, "--output", backupDir)
	adc("migrate", "legacy", "verify", backupDir)
	backup, err := legacy.Verify(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	containsTar := func(file, name, expected string) {
		t.Helper()
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		tr := tar.NewReader(f)
		for {
			h, err := tr.Next()
			if err != nil {
				t.Fatalf("missing %s: %v", name, err)
			}
			if h.Name == name {
				data, err := io.ReadAll(tr)
				if err != nil || string(data) != expected {
					t.Fatalf("wrong archive contents %s", name)
				}
				return
			}
		}
	}
	for _, r := range backup.Plan.Resources {
		if r.Source == state {
			containsTar(filepath.Join(backupDir, backup.Archives[r.ID].File), "projects", "app-a\napp-b\ninactive-project\n")
			containsTar(filepath.Join(backupDir, backup.Archives[r.ID].File), "inactive-project/codex/auth.json", "fake-inactive-account")
		}
		if r.Source == oldDinD {
			containsTar(filepath.Join(backupDir, backup.Archives[r.ID].File), "persistent.db", "fake-dind-data")
		}
		if r.Source == filepath.Join(tmp, "app-a") {
			containsTar(filepath.Join(backupDir, backup.Archives[r.ID].File), "ignored.secret", "fake-ignored-secret")
		}
	}
	adc("workspace", "init", name, "--port", freePort(), "--omniroute-port", freePort(), "--graph-port", freePort())
	first := backup.Plan.Resources[0].ID
	archiveFile := filepath.Join(backupDir, backup.Archives[first].File)
	original := []byte(read(archiveFile))
	if err = os.WriteFile(archiveFile, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = execute(cli, "migrate", "legacy", "import", backupDir, "--account", "app-a")
	if err == nil || !strings.Contains(output, "изменён") {
		t.Fatalf("imported corrupt snapshot: %v %s", err, output)
	}
	if err = os.WriteFile(archiveFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	adc("migrate", "legacy", "import", backupDir, "--account", "app-b")
	// The original bind paths, containers and old volumes all remain.
	if got := read(filepath.Join(state, "app-a", "codex", "auth.json")); got != "app-a-fake-auth" {
		t.Fatal("original auth changed")
	}
	for _, old := range oldContainers {
		docker("inspect", "--format", "{{.Id}}", old)
	}
	for _, volume := range []string{oldTools, oldDinD} {
		docker("volume", "inspect", "--format", "{{.Name}}", volume)
	}
	if _, err = legacy.Verify(backupDir); err != nil {
		t.Fatal("import changed original snapshot")
	}
	adc("workspace", "start")
	execNode := func(args ...string) string {
		t.Helper()
		return docker(append([]string{"exec", "--user", "node", newContainer}, args...)...)
	}
	for file, expected := range map[string]string{
		".codex/auth.json":                                  "app-b-fake-auth",
		".codex/sessions/chat.jsonl":                        "app-b-session",
		".codex/sessions/app-a.jsonl":                       "app-a-unique",
		".hermes/auth.json":                                 "fake-shared-hermes-auth",
		".local/share/opencode/opencode.db":                 "fake-opencode-database",
		".adc-legacy/app-a/home/.codex/sessions/chat.jsonl": "app-a-session",
	} {
		if got := strings.TrimSpace(execNode("cat", "/home/node/"+file)); got != expected {
			t.Fatalf("%s: %q != %q", file, got, expected)
		}
	}
	execNode("test", "-r", "/opt/ai-tools/share/mcp/secrets.env")
	// The usual workspace lifecycle preserves imported data.
	adc("workspace", "stop")
	adc("workspace", "start")
	if got := strings.TrimSpace(execNode("cat", "/home/node/.codex/auth.json")); got != "app-b-fake-auth" {
		t.Fatal("auth lost after restart")
	}
	if !strings.Contains(execNode("cat", "/home/node/.adc-legacy/report.json"), "app-a:.codex/sessions/chat.jsonl") {
		t.Fatal("history collision not reported")
	}
}
