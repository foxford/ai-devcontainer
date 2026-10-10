package runtime

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"go.yaml.in/yaml/v3"
)

func writeFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := shared.AtomicWrite(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOrcaSetupPreservesProjectAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "orca.yaml")
	original := "# team settings\nscripts:\n  setup: pnpm install\nworktree:\n  sharedDirectories: [node_modules]\n"
	writeFixture(t, path, original)
	if err := wireOrca(root); err != nil {
		t.Fatal(err)
	}
	first := readFixture(t, path)
	var config struct {
		Scripts  struct{ Setup string }
		Worktree struct {
			SharedDirectories []string `yaml:"sharedDirectories"`
		}
		SetupAgentStartupPolicy string `yaml:"setupAgentStartupPolicy"`
	}
	if err := yaml.Unmarshal([]byte(first), &config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "# team settings") || !strings.Contains(config.Scripts.Setup, "pnpm install") || strings.Count(config.Scripts.Setup, "adc sync") != 1 {
		t.Fatalf("project setup was changed or ADC hook duplicated:\n%s", first)
	}
	if config.SetupAgentStartupPolicy != "wait-for-setup" || !reflect.DeepEqual(config.Worktree.SharedDirectories, []string{"node_modules"}) {
		t.Fatalf("unexpected Orca config: %#v", config)
	}
	backup := filepath.Join(root, shared.ProjectStateDir, "backups", "orca.yaml")
	if readFixture(t, backup) != original {
		t.Fatal("original config not backed up exactly")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := wireOrca(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if readFixture(t, path) != first || !info.ModTime().Equal(after.ModTime()) || readFixture(t, backup) != original {
		t.Fatal("repeated setup changed data, mtime or backup")
	}
}

func TestOrcaRejectsInvalidConfigWithoutWriting(t *testing.T) {
	for _, data := range []string{"scripts: {setup: ['not a string']}\n", "scripts: []\n", "- array\n", "scripts: [\n", "scripts:\n  setup: '# adc:setup:start'\n"} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "orca.yaml")
			writeFixture(t, path, data)
			if err := wireOrca(root); err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if readFixture(t, path) != data {
				t.Fatal("invalid original config changed")
			}
			if _, err := os.Stat(filepath.Join(root, shared.ProjectStateDir, "backups", "orca.yaml")); !os.IsNotExist(err) {
				t.Fatal("backup created for a rejected update")
			}
		})
	}
}

func TestPlansDatesSpacesAndTaskPriority(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string]string{
		"2026/task with spaces/task.md": "---\nstatus: active\nupdated: 2026-10-09\n---\n",
		"2026/task with spaces/epic.md": "---\nstatus: done\nupdated: 2099-01-01\n---\n",
		"2025/previous/task.md":         "---\nstatus: done\ncreated: 2025-12-20\n---\n",
		"archive/old/epic.md":           "Old metadata-free plan\n",
	} {
		writeFixture(t, filepath.Join(root, filepath.FromSlash(path)), data)
	}
	entries, err := readPlans(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []planEntry{
		{Path: "archive/old", Status: "—", Updated: "0000-00-00", Legacy: "archive/ epic.md"},
		{Path: "2025/previous", Status: "done", Updated: "2025-12-20"},
		{Path: "2026/task with spaces", Status: "active", Updated: "2026-10-09"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("plans: %#v; want %#v", entries, want)
	}
}
