package host

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func cleanupFixture(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	a := &App{Platform: testutil.Root(t), State: filepath.Join(t.TempDir(), "state")}
	for rel, data := range map[string]string{
		".devcontainer/devcontainer.json":             "{\"name\":\"old environment\"}",
		".ai-devcontainer/platform-rev":               "old-revision\n",
		".ai-devcontainer/mcp-state":                  "playwright\n",
		".mcp.json":                                   "{\"mcpServers\":{\"playwright\":{}}}",
		".claude/skills/stock/.ai-devcontainer-wired": "",
		".claude/skills/stock/script.js":              "generated code",
		".claude/skills/personal/SKILL.md":            "my direct skill",
		".claude/settings.json":                       "my settings",
		".hermes/pipeline.yaml":                       "my project pipeline",
		".agents/skills/stock/SKILL.md":               "my fork",
		".agents/mcp.json":                            "team config",
		".agents/mcp.secrets.env":                     "FAKE_KEY=fixture",
		".agents/project.json":                        "{\"capabilities\":[\"changesets\"]}",
		"AGENTS.platform.md":                          shared.Marker + "generated rules",
		"AGENTS.skills.md":                            "<!-- Генерируется tooling/wire-agent-skills.sh. Не редактировать, не коммитить. -->\nold index",
		"MONOREPO.md":                                 "my manual monorepo documentation",
		"AGENTS.md":                                   "my project rules",
		"CLAUDE.md":                                   "my agent rules",
		".gitignore":                                  "my ignore rules",
		"node_modules/package/file":                   "project dependency",
		"graphify-out/graph.json":                     "project graph",
	} {
		cleanupWrite(t, root, rel, data)
	}
	return a, root
}

func cleanupWrite(t *testing.T, root, rel, data string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func cleanupPaths(plan cleanupPlan) map[string]bool {
	paths := map[string]bool{}
	for _, entry := range plan.Entries {
		paths[entry.Path] = true
	}
	return paths
}

func TestLegacyCleanupPreviewAndVerifiedReset(t *testing.T) {
	a, root := cleanupFixture(t)
	before, err := legacyPlatformDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.LegacyCleanup(root, false, true); err != nil {
		t.Fatal(err)
	}
	after, err := legacyPlatformDigest(root)
	if err != nil || after != before {
		t.Fatal("preview changed project")
	}
	if _, err := os.Stat(a.State); !os.IsNotExist(err) {
		t.Fatal("preview created state or backup")
	}
	plan, err := a.planLegacyCleanup(root, true)
	if err != nil {
		t.Fatal(err)
	}
	paths := cleanupPaths(plan)
	for _, rel := range []string{".devcontainer", ".ai-devcontainer", ".mcp.json", ".claude/skills/stock", "AGENTS.platform.md", "AGENTS.skills.md"} {
		if !paths[rel] {
			t.Fatalf("missing generated entry %s", rel)
		}
	}
	preserved := map[string]string{}
	for _, rel := range []string{".claude/skills/personal/SKILL.md", ".claude/settings.json", ".hermes/pipeline.yaml", ".agents/skills/stock/SKILL.md", ".agents/mcp.json", ".agents/mcp.secrets.env", ".agents/project.json", "MONOREPO.md", "AGENTS.md", "CLAUDE.md", ".gitignore", "node_modules/package/file", "graphify-out/graph.json"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		preserved[rel] = string(data)
	}
	digests := map[string]string{}
	for _, entry := range plan.Entries {
		digest, err := legacyPlatformDigest(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			t.Fatal(err)
		}
		digests[entry.Path] = digest
	}
	backup, err := a.applyLegacyCleanup(plan, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for rel, digest := range digests {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("generated entry remains: %s", rel)
		}
		copied, err := legacyPlatformDigest(filepath.Join(backup, "checkout", filepath.FromSlash(rel)))
		if err != nil || copied != digest {
			t.Fatalf("original data missing from backup: %s", rel)
		}
	}
	for rel, expected := range preserved {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || string(data) != expected {
			t.Fatalf("changed user data %s", rel)
		}
	}
	data, err := os.ReadFile(filepath.Join(backup, "cleanup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest cleanupPlan
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Status != "complete" {
		t.Fatal("missing completion manifest")
	}
	for _, entry := range manifest.Entries {
		if entry.Digest != digests[entry.Path] {
			t.Fatal("manifest lost source checksum")
		}
	}
}

func TestLegacyCleanupKeepsForeignMCPAndOptInDevcontainer(t *testing.T) {
	a, root := cleanupFixture(t)
	cleanupWrite(t, root, ".ai-devcontainer/mcp-state.json", `{"version":1,"claudeOwned":false,"servers":[]}`)
	plan, err := a.planLegacyCleanup(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if paths := cleanupPaths(plan); paths[".mcp.json"] || paths[".devcontainer"] {
		t.Fatal("selected foreign MCP or non-selected devcontainer")
	}
}

func TestLegacyCleanupRefusesWritersAndChangesBeforeRemoval(t *testing.T) {
	for _, kind := range []string{"running", "started during copy", "edited during copy", "ownership changed before copy", "ownership changed during copy", "backup inside project"} {
		t.Run(kind, func(t *testing.T) {
			a, root := cleanupFixture(t)
			plan, err := a.planLegacyCleanup(root, true)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			stopped := func(string) error {
				calls++
				if kind == "running" || kind == "started during copy" && calls == 2 {
					return errors.New("active writer")
				}
				if kind == "edited during copy" && calls == 2 {
					cleanupWrite(t, root, "AGENTS.platform.md", shared.Marker+"new user edit")
				}
				if kind == "ownership changed before copy" && calls == 1 || kind == "ownership changed during copy" && calls == 2 {
					cleanupWrite(t, root, "AGENTS.platform.md", "manual project rules")
				}
				return nil
			}
			if kind == "backup inside project" {
				a.State = filepath.Join(root, "state")
			}
			if _, err := a.applyLegacyCleanup(plan, stopped); err == nil {
				t.Fatal("accepted unsafe cleanup")
			}
			for _, entry := range plan.Entries {
				if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(entry.Path))); err != nil {
					t.Fatalf("removed original %s before all checks", entry.Path)
				}
			}
		})
	}
}

func TestLegacyCleanupRejectsBackupInsideAliasedProject(t *testing.T) {
	for _, aliasProject := range []bool{false, true} {
		name := "backup alias"
		if aliasProject {
			name = "project alias"
		}
		t.Run(name, func(t *testing.T) {
			a, root := cleanupFixture(t)
			alias := filepath.Join(t.TempDir(), "project")
			if err := os.Symlink(root, alias); err != nil {
				t.Skipf("directory symlink unavailable: %v", err)
			}
			projectRoot, stateRoot := root, alias
			if aliasProject {
				projectRoot, stateRoot = alias, root
			}
			a.State = filepath.Join(stateRoot, "state")
			plan, err := a.planLegacyCleanup(projectRoot, true)
			if err != nil {
				t.Fatal(err)
			}
			before, err := legacyPlatformDigest(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.applyLegacyCleanup(plan, func(string) error { return nil }); err == nil {
				t.Fatal("accepted backup inside aliased project")
			}
			after, err := legacyPlatformDigest(root)
			if err != nil || before != after {
				t.Fatal("unsafe cleanup modified project")
			}
		})
	}
}

func TestLegacyCleanupPreservesNewState(t *testing.T) {
	a, root := cleanupFixture(t)
	if err := shared.MigrateProjectState(root); err != nil {
		t.Fatal(err)
	}
	before, err := shared.TreeDigest(filepath.Join(root, shared.ProjectStateDir))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := a.planLegacyCleanup(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.applyLegacyCleanup(plan, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	after, err := shared.TreeDigest(filepath.Join(root, shared.ProjectStateDir))
	if err != nil || before != after {
		t.Fatal("cleanup lost new ownership or fork bases")
	}
}

func TestLegacyCleanupDoesNotFollowLinks(t *testing.T) {
	a, root := cleanupFixture(t)
	outside := t.TempDir()
	cleanupWrite(t, outside, "README.md", "outside source")
	link := filepath.Join(root, ".agents", "skills", "README.md")
	if err := os.Symlink(filepath.Join(a.Platform, "docs", "skills-README.md"), link); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "plans")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dsh", "skills"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, ".claude", "skills", "stock"), filepath.Join(root, ".dsh", "skills", "stock")); err != nil {
		t.Fatal(err)
	}
	plan, err := a.planLegacyCleanup(root, true)
	if err != nil {
		t.Fatal(err)
	}
	paths := cleanupPaths(plan)
	if !paths[".agents/skills/README.md"] || !paths[".dsh/skills/stock"] || paths["plans/README.md"] {
		t.Fatal("wrong symlink ownership")
	}
	backup, err := a.applyLegacyCleanup(plan, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(outside, "README.md")); err != nil || string(data) != "outside source" {
		t.Fatal("modified external target")
	}
	if _, err := os.Readlink(filepath.Join(backup, "checkout", ".dsh", "skills", "stock")); err != nil {
		t.Fatal("backup followed the link")
	}
}

func TestLegacyCleanupGuardsPlatformReadOnlyAndNestedState(t *testing.T) {
	a, root := cleanupFixture(t)
	if _, err := a.cleanupRoot(a.Platform); err == nil {
		t.Fatal("allowed cleanup of platform")
	}
	config := shared.Config{Version: 1, Name: "developer", Port: 6768, Projects: []shared.Project{{ID: "app", Path: root, RuntimePath: "/projects/app", ReadOnly: true}}}
	if err := a.shared().Save(config); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"app", root} {
		if _, err := a.cleanupRoot(target); err == nil {
			t.Fatal("allowed read-only project cleanup")
		}
	}
	a.State = filepath.Join(root, "state")
	if err := a.LegacyCleanup(root, true, true); err == nil {
		t.Fatal("accepted nested ADC_HOME")
	}
	if _, err := os.Stat(a.State); !os.IsNotExist(err) {
		t.Fatal("created state inside project before rejecting")
	}
}
