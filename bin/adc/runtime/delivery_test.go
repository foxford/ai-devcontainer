package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func requireSymlinks(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.Symlink(root, filepath.Join(root, "link")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlinks require Developer Mode or elevated privileges")
		}
		t.Fatal(err)
	}
}

func TestDeliveryRepairsLegacyDSHLinksAndPreservesForeignLinks(t *testing.T) {
	requireSymlinks(t)
	a := &App{Platform: testutil.Delivery(t), Runtime: true}
	root := t.TempDir()
	writeFixture(t, filepath.Join(a.Platform, "delivery", "skills", "example", "SKILL.md"), "---\nname: example\n---\nstock")
	dsh := filepath.Join(root, ".dsh", "skills")
	if err := os.MkdirAll(dsh, 0755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"example": "/workspaces/" + filepath.Base(root) + "/.claude/skills/example", "graphify": "/workspaces/" + filepath.Base(root) + "/.claude/skills/graphify", "personal": "/custom/personal"} {
		if err := os.Symlink(target, filepath.Join(dsh, name)); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := a.DeliverSkills(root, []string{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dsh, "example", "SKILL.md")); err != nil {
		t.Fatal("active link was not repaired", err)
	}
	if _, err := os.Lstat(filepath.Join(dsh, "graphify")); !os.IsNotExist(err) {
		t.Fatal("obsolete delivered link retained", err)
	}
	if link, err := os.Readlink(filepath.Join(dsh, "personal")); err != nil || link != "/custom/personal" {
		t.Fatal("foreign link modified", link, err)
	}
}

func TestDocumentSelectionAndOwnership(t *testing.T) {
	requireSymlinks(t)
	a := &App{Platform: testutil.Delivery(t), Runtime: true}
	p := shared.Project{RuntimePath: t.TempDir(), Capabilities: []string{"pnpm-nx"}}
	if err := a.DeliverDocuments(p); err != nil {
		t.Fatal(err)
	}
	monorepo := filepath.Join(p.RuntimePath, "MONOREPO.md")
	if target, err := os.Readlink(monorepo); err != nil || target != a.shared().DeliveryPath("documents/monorepo/README.md") {
		t.Fatalf("wrong capability document: %s %v", target, err)
	}
	p.Capabilities = nil
	if err := a.DeliverDocuments(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(monorepo); !os.IsNotExist(err) {
		t.Fatal("inactive owned capability document not removed")
	}
	writeFixture(t, monorepo, "project owns this document\n")
	readme := filepath.Join(p.RuntimePath, "plans", "README.md")
	if err := os.Remove(readme); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, readme, "project plans\n")
	for _, capabilities := range [][]string{{"pnpm-nx"}, nil} {
		p.Capabilities = capabilities
		if err := a.DeliverDocuments(p); err != nil {
			t.Fatal(err)
		}
		if readFixture(t, monorepo) != "project owns this document\n" || readFixture(t, readme) != "project plans\n" {
			t.Fatal("foreign document was overwritten or removed")
		}
	}
}

func TestLegacySkillReadmeAndForeignSkillDoNotBlockDelivery(t *testing.T) {
	requireSymlinks(t)
	a := &App{Platform: testutil.Delivery(t), Runtime: true}
	root := t.TempDir()
	stock := a.shared().DeliveryPath("skills/example")
	writeFixture(t, filepath.Join(stock, "SKILL.md"), "platform instructions\n")
	readme := filepath.Join(root, ".agents", "skills", "README.md")
	if err := os.MkdirAll(filepath.Dir(readme), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/opt/dev-platform/docs/skills-README.md", readme); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, ".claude", "skills", "example")
	writeFixture(t, filepath.Join(foreign, "SKILL.md"), "my custom instructions\n")
	writeFixture(t, filepath.Join(foreign, "scripts", "personal.py"), "my custom code\n")
	before, err := shared.TreeDigest(foreign)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := a.DeliverSkills(root); err != nil {
			t.Fatal(err)
		}
		if err := a.DeliverDocuments(shared.Project{RuntimePath: root}); err != nil {
			t.Fatal(err)
		}
		after, err := shared.TreeDigest(foreign)
		if err != nil || before != after {
			t.Fatal("foreign skill modified")
		}
		if readFixture(t, readme) != readFixture(t, a.shared().DeliveryPath("documents/skills/README.md")) {
			t.Fatal("legacy README not repaired")
		}
		if !strings.Contains(readFixture(t, filepath.Join(root, "AGENTS.skills.md")), "пользовательский каталог") {
			t.Fatal("foreign skill not identified in index")
		}
	}
	// A similarly named document outside the known platform roots is retained.
	if err := os.Remove(readme); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "docs", "skills-README.md")
	writeFixture(t, outside, "team documentation\n")
	if err := os.Symlink(outside, readme); err != nil {
		t.Fatal(err)
	}
	if err := a.DeliverDocuments(shared.Project{RuntimePath: root}); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(readme); err != nil || link != outside {
		t.Fatal("foreign README link replaced")
	}
}

func TestSkillOverlayCopiesCodeAndLinksProse(t *testing.T) {
	requireSymlinks(t)
	a := &App{Platform: testutil.Delivery(t), Runtime: true}
	root := t.TempDir()
	stock := a.shared().DeliveryPath("skills/example")
	for path, data := range map[string]string{
		"SKILL.md":      "---\nname: example\ndescription: 'Stock | description'\n---\nstock instructions\n",
		"reference.md":  "stock reference\n",
		"scripts/a.mjs": "import './b.mjs';\n",
		"scripts/b.mjs": "export const value = 'stock';\n",
	} {
		writeFixture(t, filepath.Join(stock, filepath.FromSlash(path)), data)
	}
	project := filepath.Join(root, ".agents", "skills", "example")
	writeFixture(t, filepath.Join(project, "reference.md"), "fork reference\n")
	writeFixture(t, filepath.Join(project, "scripts", "b.mjs"), "export const value = 'fork';\n")
	if err := a.DeliverSkills(root); err != nil {
		t.Fatal(err)
	}
	composed := filepath.Join(root, ".claude", "skills", "example")
	for rel, want := range map[string]string{"SKILL.md": filepath.Join(stock, "SKILL.md"), "reference.md": filepath.Join(project, "reference.md")} {
		if got, err := os.Readlink(filepath.Join(composed, rel)); err != nil || got != want {
			t.Fatalf("prose link %s: %s %v", rel, got, err)
		}
	}
	for _, rel := range []string{"scripts/a.mjs", "scripts/b.mjs"} {
		info, err := os.Lstat(filepath.Join(composed, filepath.FromSlash(rel)))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("code was not copied: %s %v", rel, err)
		}
	}
	if !strings.Contains(readFixture(t, filepath.Join(composed, "scripts", "b.mjs")), "'fork'") || !strings.Contains(readFixture(t, filepath.Join(root, "AGENTS.skills.md")), `Stock \| description`) {
		t.Fatal("fork not applied or index table escaping broken")
	}
	writeFixture(t, filepath.Join(stock, "reference.md"), "updated stock\n")
	if err := os.Remove(filepath.Join(project, "reference.md")); err != nil {
		t.Fatal(err)
	}
	if err := a.DeliverSkills(root); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, filepath.Join(composed, "reference.md")) != "updated stock\n" {
		t.Fatal("unforked prose did not return to stock layer")
	}
	foreign := filepath.Join(root, ".claude", "skills", "foreign")
	writeFixture(t, filepath.Join(foreign, "SKILL.md"), "project owned\n")
	if err := os.RemoveAll(stock); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	if err := a.DeliverSkills(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(composed); !os.IsNotExist(err) {
		t.Fatal("obsolete managed skill not removed")
	}
	if _, err := os.Lstat(filepath.Join(root, ".dsh", "skills", "example")); !os.IsNotExist(err) {
		t.Fatal("obsolete DSH link not removed")
	}
	if readFixture(t, filepath.Join(foreign, "SKILL.md")) != "project owned\n" {
		t.Fatal("foreign skill removed")
	}
}

func TestSkillForkBaselineUnforkAndTraversal(t *testing.T) {
	a := &App{Platform: testutil.Delivery(t), Runtime: true}
	root := t.TempDir()
	stock := a.shared().DeliveryPath("skills/example")
	writeFixture(t, filepath.Join(stock, "SKILL.md"), "stock\n")
	if err := a.Skill(root, []string{"fork", "example"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, ".agents", "skills", "example", "SKILL.md")
	base := filepath.Join(root, shared.ProjectStateDir, "skills-base", "example", "SKILL.md")
	if readFixture(t, project) != "stock\n" || readFixture(t, base) != "stock\n" {
		t.Fatal("fork or baseline missing")
	}
	writeFixture(t, project, "project modified\n")
	if err := a.Skill(root, []string{"fork", "example"}); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, project) != "project modified\n" {
		t.Fatal("fork overwritten by repeated command")
	}
	for _, files := range [][]string{{"SKILL.md", "../../escape"}, {"../escape"}, {"/tmp/escape"}, {`C:\escape`}} {
		if err := a.Skill(root, append([]string{"fork", "example"}, files...)); err == nil {
			t.Fatal("unsafe path accepted", files)
		}
	}
	if err := a.Skill(root, []string{"unfork", "example"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{project, base} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unfork left project or baseline file", path)
		}
	}
}

func TestSyncIgnoresOwnedDocumentsAndPreservesProjectText(t *testing.T) {
	requireSymlinks(t)
	a, p, _ := mcpFixture(t)
	t.Setenv("ADC_INSTALL_AI_TOOLS", "0")
	p.Capabilities = []string{"pnpm-nx"}
	writeFixture(t, filepath.Join(p.RuntimePath, ".gitignore"), "project-cache/\n")
	writeFixture(t, filepath.Join(p.RuntimePath, "AGENTS.md"), "Project-specific rules\n")
	// Foreign documents are retained in Git; only generated outputs are ignored.
	writeFixture(t, filepath.Join(p.RuntimePath, "plans", "README.md"), "Team owns this README\n")
	if err := a.SyncProject(p); err != nil {
		t.Fatal(err)
	}
	ignore := readFixture(t, filepath.Join(p.RuntimePath, ".gitignore"))
	for _, pattern := range []string{"project-cache/", "/MONOREPO.md", "/.agents/skills/README.md", "/.agents/mcp.secrets.env.example", "/.agents/mcp.secrets.env"} {
		if !strings.Contains(ignore, pattern) {
			t.Fatal("missing ignore pattern", pattern)
		}
	}
	if strings.Contains(ignore, "/plans/README.md") || !strings.Contains(readFixture(t, filepath.Join(p.RuntimePath, "AGENTS.md")), "Project-specific rules") {
		t.Fatal("foreign document hidden or project rules lost")
	}
	first := ignore
	if err := a.SyncProject(p); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, filepath.Join(p.RuntimePath, ".gitignore")) != first {
		t.Fatal("repeated sync duplicated ignore patterns")
	}
}
