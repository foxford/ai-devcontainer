package host

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func inspectFixture(t *testing.T, id, project, folder string) legacyInspection {
	t.Helper()
	value := map[string]any{
		"Id": id, "Name": "/" + id,
		"Config": map[string]any{"Labels": map[string]string{"devcontainer.local_folder": folder}},
		"Mounts": []map[string]string{
			{"Type": "bind", "Source": "/home/user/Development/" + project, "Destination": "/workspaces/" + project},
			{"Type": "bind", "Source": "/home/user/.ai-devcontainer-dev/" + project + "/codex", "Destination": "/home/node/.codex"},
			{"Type": "volume", "Source": "/var/lib/docker/volumes/old-tools/_data", "Name": "old-tools", "Destination": "/opt/ai-tools"},
			{"Type": "volume", "Source": "/var/lib/docker/volumes/" + project + "-dind/_data", "Name": project + "-dind", "Destination": "/var/lib/docker"},
			{"Type": "bind", "Source": "/tmp/wayland", "Destination": "/tmp/wayland.sock"},
		},
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var inspected legacyInspection
	if err = json.Unmarshal(data, &inspected); err != nil {
		t.Fatal(err)
	}
	return inspected
}

func TestLegacyPlanUsesMountsForWSLAndIncludesWholeStateRoot(t *testing.T) {
	a := inspectFixture(t, "container-a", "portal-frontend", `\\wsl.localhost\Foxbuntu\home\user\Development\portal-frontend`)
	b := inspectFixture(t, "container-b", "frontend-platform", "/home/user/Development/frontend-platform")
	plan, err := legacyPlan([]legacyInspection{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Containers[1].ProjectPath != "/home/user/Development/portal-frontend" {
		t.Fatal("used UNC label as host mount source")
	}
	count := 0
	root := false
	for _, r := range plan.Resources {
		if r.Source == "old-tools" {
			count++
		}
		if r.Source == "/home/user/.ai-devcontainer-dev" {
			root = true
		}
	}
	if count != 1 || !root {
		t.Fatal("shared volume not deduplicated or parent registry omitted")
	}
	for _, container := range plan.Containers {
		for _, m := range container.Mounts {
			if m.Destination == "/tmp/wayland.sock" && (m.Resource != "" || m.Skip == "") {
				t.Fatal("ephemeral socket included without explanation")
			}
		}
	}
	for _, r := range plan.Resources {
		if r.Type == "volume" && strings.Contains(r.Source, "_data") {
			t.Fatal("used daemon-local volume path")
		}
	}
}

func TestLegacyPlanRefusesUnknownAndDuplicateCheckouts(t *testing.T) {
	a := inspectFixture(t, "container-a", "app", "/projects/app")
	for _, kind := range []string{"missing-label", "duplicate-project", "missing-checkout"} {
		t.Run(kind, func(t *testing.T) {
			one := a
			inspections := []legacyInspection{one}
			switch kind {
			case "missing-label":
				one.Config.Labels = nil
				inspections[0] = one
			case "duplicate-project":
				other := a
				other.ID = "container-b"
				inspections = append(inspections, other)
			case "missing-checkout":
				one.Mounts = nil
				inspections[0] = one
			}
			if _, err := legacyPlan(inspections); err == nil {
				t.Fatal("accepted ambiguous migration")
			}
		})
	}
}

func TestBootstrapRefusesToPullOverV0(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX bootstrap")
	}
	old := t.TempDir()
	for _, name := range []string{".git", "bin"} {
		if err := os.Mkdir(filepath.Join(old, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(old, "bin/adc")
	if err := os.WriteFile(file, []byte("legacy executable"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", filepath.Join(testutil.Root(t), "install.sh"), "--no-shell")
	cmd.Env = append(os.Environ(), "AI_DEVCONTAINER_HOME="+old, "ADC_HOME="+filepath.Join(t.TempDir(), "new"))
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "Legacy ADC detected") {
		t.Fatalf("bootstrap did not stop before dependencies/pull: %v %s", err, output)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "legacy executable" {
		t.Fatal("bootstrap changed original")
	}
}

func TestLegacyPlanIgnoresDockerMountOrder(t *testing.T) {
	a := inspectFixture(t, "container-a", "app-a", "/projects/app-a")
	b := inspectFixture(t, "container-b", "app-b", "/projects/app-b")
	first, err := legacyPlan([]legacyInspection{a, b})
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(a.Mounts)-1; i < j; i, j = i+1, j-1 {
		a.Mounts[i], a.Mounts[j] = a.Mounts[j], a.Mounts[i]
	}
	second, err := legacyPlan([]legacyInspection{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("inspect mount/container order changed migration plan")
	}
}

func TestLegacyOverlapRecognizesParentsWithoutPrefixConfusion(t *testing.T) {
	for _, paths := range [][2]string{{"/home/user/.ai-devcontainer-dev", "/home/user/.ai-devcontainer-dev/app/codex"}, {"C:/Users/dev/state", "c:/users/dev/state/app"}} {
		if !overlappingLegacyPaths(paths[0], paths[1]) || !overlappingLegacyPaths(paths[1], paths[0]) {
			t.Fatal("missed nested writer")
		}
	}
	if overlappingLegacyPaths("/projects/app", "/projects/app-two") {
		t.Fatal("prefix mistaken for parent")
	}
}

func TestLegacyPendingBlocksStartBeforeDocker(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "migration-pending.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	a := (*App)(&shared.App{State: state})
	if err := a.Start(shared.Config{Name: "migration"}); err == nil || !strings.Contains(err.Error(), "миграция не завершена") {
		t.Fatalf("started incomplete migration: %v", err)
	}
}
