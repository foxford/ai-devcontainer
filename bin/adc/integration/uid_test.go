package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func TestDockerRuntimeRemapsIndependentVolumeOwners(t *testing.T) {
	if os.Getenv("ADC_INTEGRATION") != "1" || runtime.GOOS != "linux" {
		t.Skip("set ADC_INTEGRATION=1 on Linux")
	}
	root := testutil.Root(t)
	state := t.TempDir()
	name := fmt.Sprintf("adc-uid-%d-%d", os.Getpid(), time.Now().UnixNano())
	volumes := []string{name + "-home", name + "-tools", name + "-outside"}
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, data)
		}
		return string(data)
	}
	t.Cleanup(func() {
		if t.Failed() {
			data, _ := exec.Command("docker", "logs", "--tail", "80", name).CombinedOutput()
			t.Logf("runtime: %s", data)
		}
		// This fixture deliberately uses a different UID than the host. Restore
		// only its own bind directory so testing.TempDir can clean it up.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		data, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "chown", "--mount", "type=bind,src="+state+",dst=/restore", "adc-runtime:local", "-R", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "/restore").CombinedOutput()
		cancel()
		if err != nil {
			t.Logf("restore fixture owner: %v %s", err, data)
		}
		for _, args := range [][]string{{"rm", "-f", name}, append([]string{"volume", "rm"}, volumes...)} {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			data, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
			cancel()
			if err != nil {
				t.Logf("cleanup: %v %s", err, data)
			}
		}
	})
	run("build", "--build-arg", "ADC_INSTALL_AI_TOOLS=0", "-f", filepath.Join(root, "runtime/Dockerfile"), "-t", "adc-runtime:local", root)
	for _, volume := range volumes {
		run("volume", "create", volume)
	}
	mounts := []string{"--mount", "type=volume,src=" + volumes[0] + ",dst=/home/node", "--mount", "type=volume,src=" + volumes[1] + ",dst=/opt/ai-tools", "--mount", "type=volume,src=" + volumes[2] + ",dst=/outside"}
	seed := append([]string{"run", "--rm", "--entrypoint", "sh"}, mounts...)
	seed = append(seed, "adc-runtime:local", "-c", `mkdir -p /opt/ai-tools/bin; printf 'unchanged\n' > /outside/protected; chown 1000:1000 /outside/protected; ln -s /outside/protected /opt/ai-tools/external; chown -R 1000:1000 /home/node /opt/ai-tools`)
	run(seed...)
	if err := os.WriteFile(filepath.Join(state, "workspace.json"), []byte(`{"version":1,"name":"uid-fixture","port":6768,"projects":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	start := append([]string{"run", "-d", "--name", name}, mounts...)
	start = append(start, "--mount", "type=bind,src="+root+",dst=/opt/ai-devcontainer,readonly", "--mount", "type=bind,src="+state+",dst=/var/lib/adc", "-e", "ADC_UID=12345", "-e", "ADC_GID=12345", "-e", "ADC_INSTALL_AI_TOOLS=0", "adc-runtime:local")
	run(start...)
	for attempt := 0; attempt < 90; attempt++ {
		if err := exec.Command("docker", "exec", name, "test", "-e", "/tmp/adc-ready").Run(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	run("exec", name, "test", "-e", "/tmp/adc-ready")
	result := run("exec", "--user", "node", name, "sh", "-c", `test "$(id -u)" = 12345 && test "$(id -g)" = 12345 && printf 'writable\n' > /opt/ai-tools/bin/uid-test && printf 'writable\n' > /home/node/uid-test && test "$(stat -c %u /outside/protected)" = 1000 && cat /opt/ai-tools/external && printf 'ADC_UID_OK\n'`)
	if !strings.Contains(result, "ADC_UID_OK") || !strings.Contains(result, "unchanged") {
		t.Fatal("UID remap lost writes or followed an external symlink")
	}
}
