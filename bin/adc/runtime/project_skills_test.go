package runtime

import (
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectSkillsKeepCommonHomeIndependentOfSyncOrder(t *testing.T) {
	home := testutil.Home(t)
	a := &App{Platform: testutil.Delivery(t)}
	stock := a.shared().DeliveryPath("skills/demo/SKILL.md")
	if err := shared.AtomicWrite(stock, []byte("Stock skill\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".codex", "skills")
	personal := filepath.Join(dest, "personal", "SKILL.md")
	if err := shared.AtomicWrite(personal, []byte("Developer skill\n"), 0644); err != nil {
		t.Fatal(err)
	}
	projects := []shared.Project{
		{ID: "a", RuntimePath: t.TempDir()},
		{ID: "b", RuntimePath: t.TempDir()},
		{ID: "a", RuntimePath: t.TempDir()}, // A worktree has its own scope.
	}
	namespaces := []string{"a-main", "b-main", "a-worktree"}
	for _, p := range projects {
		if err := shared.AtomicWrite(filepath.Join(p.RuntimePath, ".agents", "skills", "demo", "SKILL.md"), []byte(p.RuntimePath), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []int{0, 1, 2, 1, 0} {
		if err := a.PublishProjectSkills(projects[index], namespaces[index]); err != nil {
			if runtime.GOOS == "windows" && os.IsPermission(err) {
				t.Skip("Windows symlink privilege is unavailable:", err)
			}
			t.Fatal(err)
		}
		link, err := os.Readlink(filepath.Join(dest, "demo"))
		if err != nil || link != filepath.Dir(stock) {
			t.Fatalf("common skill depends on last checkout: %q, %v", link, err)
		}
	}
	for index, namespace := range namespaces {
		body, err := os.ReadFile(filepath.Join(dest, namespace+"--demo", "SKILL.md"))
		if err != nil || !strings.Contains(string(body), projects[index].RuntimePath) {
			t.Fatalf("project adapter lost its scope: %s, %v", namespace, err)
		}
	}
	// Removing the entire overrides directory must remove only its adapters.
	if err := os.RemoveAll(filepath.Join(projects[0].RuntimePath, ".agents", "skills")); err != nil {
		t.Fatal(err)
	}
	if err := a.PublishProjectSkills(projects[0], namespaces[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a-main--demo")); !os.IsNotExist(err) {
		t.Fatalf("removed fork is still advertised: %v", err)
	}
	for _, path := range []string{personal, filepath.Join(dest, "b-main--demo", "SKILL.md"), filepath.Join(dest, "a-worktree--demo", "SKILL.md"), filepath.Join(dest, "demo", "SKILL.md")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cleanup changed another skill: %s, %v", path, err)
		}
	}
}
