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
	devices := t.TempDir()
	device := filepath.Join(devices, "usb")
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
	// This isolated char device behaves like /dev/null. Its restrictive owner
	// and group exercise USB access without requiring hardware or changing a
	// real host device. The containing directory remains owned by the test.
	run("run", "--rm", "--network", "none", "--entrypoint", "sh", "--mount", "type=bind,src="+devices+",dst=/fixture", "adc-runtime:local", "-c", `mknod /fixture/usb c 1 3 && chown 0:45678 /fixture/usb && chmod 0660 /fixture/usb`)
	deviceMetadata := func() string {
		t.Helper()
		return strings.TrimSpace(run("run", "--rm", "--network", "none", "--entrypoint", "stat", "--mount", "type=bind,src="+devices+",dst=/fixture,readonly", "adc-runtime:local", "-c", "%u:%g:%a", "/fixture/usb"))
	}
	if metadata := deviceMetadata(); metadata != "0:45678:660" {
		t.Fatalf("unexpected device fixture permissions: %s", metadata)
	}
	// Prove that the image's ordinary user cannot open the device before ADC
	// adds its host group; world-writable fixtures would hide that regression.
	run("run", "--rm", "--network", "none", "--device", device+":/dev/adc-usb-test:rw", "--entrypoint", "runuser", "adc-runtime:local", "-u", "node", "--", "sh", "-c", `if (printf 'probe\n' > /dev/adc-usb-test) 2>/dev/null; then exit 1; fi`)
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
	start = append(start, "--mount", "type=bind,src="+root+",dst=/opt/ai-devcontainer,readonly", "--mount", "type=bind,src="+state+",dst=/var/lib/adc", "--device", device+":/dev/adc-usb-test:rw", "-e", "ADC_UID=12345", "-e", "ADC_GID=12345", "-e", "ADC_USB_GIDS=45678", "-e", "ADC_INSTALL_AI_TOOLS=0", "adc-runtime:local")
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
	probe := `test "$(id -u)" = 12345 && test "$(stat -c %u:%g:%a /dev/adc-usb-test)" = 0:45678:660 && test -c /dev/adc-usb-test && dd if=/dev/adc-usb-test of=/dev/null bs=1 count=1 status=none && printf 'writable\n' > /dev/adc-usb-test && printf 'ADC_USB_OK\n'`
	for _, args := range [][]string{
		{"exec", "--user", "node", name, "sh", "-c", probe},
		{"exec", name, "runuser", "-u", "node", "--", "sh", "-c", probe},
	} {
		if output := run(args...); !strings.Contains(output, "ADC_USB_OK") {
			t.Fatalf("non-root device probe did not complete: %s", output)
		}
	}
	if output := run("exec", "--user", "node", name, "lsusb", "--version"); !strings.Contains(output, "usbutils") {
		t.Fatalf("lsusb is unavailable: %s", output)
	}
	if metadata := deviceMetadata(); metadata != "0:45678:660" {
		t.Fatalf("runtime changed host device permissions: %s", metadata)
	}
}
