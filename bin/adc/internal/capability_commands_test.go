package shared

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func capabilityFixture(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	if err := Run(root, "git", "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	a := &App{Platform: testutil.Delivery(t), State: t.TempDir()}
	c := Config{Version: Schema, Name: "test", Port: 6768, Projects: []Project{{ID: "app", Path: root, RuntimePath: "/workspace/projects/app", Capabilities: []string{"changesets"}}}}
	if err := a.Save(c); err != nil {
		t.Fatal(err)
	}
	return a, root
}

func TestCapabilityChangesPreserveSettingsAndRejectInvalidComposition(t *testing.T) {
	a, root := capabilityFixture(t)
	path := filepath.Join(root, ".agents", "project.json")
	if err := AtomicWrite(path, []byte(`{"capabilities":["pnpm-nx"],"custom":{"retain":true},"large":9007199254740993}`), 0600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.ChangeCapabilities(nested, "add", []string{"frontend", "frontend", "changesets"}); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	var team []string
	_ = json.Unmarshal(settings["capabilities"], &team)
	if strings.Join(team, ",") != "pnpm-nx,frontend" || string(settings["large"]) != "9007199254740993" || !strings.Contains(string(settings["custom"]), "true") {
		t.Fatalf("settings lost or duplicated: %s", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("existing permissions changed")
	}
	catalog, _ := a.DeliveryCatalog()
	catalog.Capabilities["blocked"] = DeliveryCapability{Description: "test conflict", Instructions: []string{"instructions/capabilities/figma.md"}, Conflicts: []string{"frontend"}}
	catalogData, _ := json.Marshal(catalog)
	if err := os.WriteFile(a.DeliveryPath("catalog.json"), catalogData, 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"unknown"}, {"figma", "unknown"}, {"blocked"}} {
		if _, err := a.ChangeCapabilities(root, "add", args); err == nil {
			t.Fatal("invalid selection accepted:", args)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(after, data) {
			t.Fatal("invalid selection changed project settings")
		}
	}
	if _, err := a.ChangeCapabilities(root, "rm", []string{"changesets"}); err == nil {
		t.Fatal("silently removed a registration capability")
	}
	if _, err := a.ChangeCapabilities(root, "rm", []string{"frontend"}); err != nil {
		t.Fatal(err)
	}
	p, err := a.CapabilityProject(root)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := a.ProjectCapabilities(p)
	if err != nil || strings.Join(selected, ",") != "changesets,pnpm-nx" {
		t.Fatal(selected, err)
	}
	c, _ := a.Load()
	c.Projects[0].ReadOnly = true
	_ = a.Save(c)
	before, _ := os.ReadFile(path)
	if _, err := a.ChangeCapabilities(root, "add", []string{"figma"}); err == nil {
		t.Fatal("read-only project changed")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("read-only rejection changed settings")
	}
}

func TestCapabilityCatalogMarksEffectiveSelectionAndWorksWithoutWorkspace(t *testing.T) {
	a, root := capabilityFixture(t)
	if _, err := a.ChangeCapabilities(root, "add", []string{"graphify"}); err != nil {
		t.Fatal(err)
	}
	for _, color := range []bool{false, true} {
		var out bytes.Buffer
		if err := a.WriteCapabilities(root, &out, color); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"changesets", "graphify", "frontend", "obsidian"} {
			if !strings.Contains(out.String(), name) {
				t.Fatal("catalog missing", name)
			}
		}
		if strings.Count(out.String(), "✓") != 3 || strings.Contains(out.String(), "\x1b[32m✓\x1b[0m") != color {
			t.Fatal("wrong selection or color:", out.String())
		}
	}
	a.State = t.TempDir()
	var out bytes.Buffer
	if err := a.WriteCapabilities(root, &out, false); err != nil || !strings.Contains(out.String(), "текущий проект не выбран") {
		t.Fatal(out.String(), err)
	}
	if _, err := a.ChangeCapabilities(root, "add", []string{"figma"}); err == nil {
		t.Fatal("unregistered project changed")
	}
}

func TestCapabilityMutationRejectsDamagedOrRedirectedConfig(t *testing.T) {
	for _, body := range []string{"null", "[]", "{", `{"capabilities":42}`} {
		t.Run(body, func(t *testing.T) {
			a, root := capabilityFixture(t)
			path := filepath.Join(root, ".agents", "project.json")
			_ = AtomicWrite(path, []byte(body), 0644)
			if _, err := a.ChangeCapabilities(root, "add", []string{"figma"}); err == nil {
				t.Fatal("invalid config accepted")
			}
			after, _ := os.ReadFile(path)
			if string(after) != body {
				t.Fatal("damaged config overwritten")
			}
		})
	}
	a, root := capabilityFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := a.ChangeCapabilities(root, "add", []string{"figma"}); err == nil {
		t.Fatal("redirected config accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "project.json")); !os.IsNotExist(err) {
		t.Fatal("wrote outside checkout")
	}
}

func TestCapabilitiesFollowCurrentWorktree(t *testing.T) {
	a, root := capabilityFixture(t)
	if err := Run(root, "git", "-c", "user.name=ADC", "-c", "user.email=adc@example.invalid", "commit", "--allow-empty", "-qm", "fixture"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "task")
	if err := Run(root, "git", "worktree", "add", "-qb", "task", worktree); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ChangeCapabilities(worktree, "add", []string{"graphify"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "project.json")); !os.IsNotExist(err) {
		t.Fatal("worktree selection changed primary checkout")
	}
	p, _ := a.CapabilityProject(worktree)
	selected, err := a.ProjectCapabilities(p)
	if err != nil || !Contains(selected, "graphify") || !Contains(selected, "changesets") {
		t.Fatal(selected, err)
	}
	// An explicitly registered checkout takes priority over a sibling with the
	// same Git common dir, including its permissions and local selection.
	c, _ := a.Load()
	c.Projects = append(c.Projects, Project{ID: "task", Path: worktree, RuntimePath: "/workspace/projects/task", ReadOnly: true})
	if err := a.Save(c); err != nil {
		t.Fatal(err)
	}
	p, err = a.CapabilityProject(worktree)
	if err != nil || p.ID != "task" || !p.ReadOnly {
		t.Fatal(p, err)
	}
	if _, err := a.ChangeCapabilities(worktree, "add", []string{"figma"}); err == nil {
		t.Fatal("selected writable sibling instead of registered read-only checkout")
	}
}
