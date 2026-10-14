package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func checkoutGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, output)
	}
}

func checkoutRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	checkoutGit(t, root, "init", "-q", "-b", "main")
	checkoutGit(t, root, "-c", "user.name=ADC", "-c", "user.email=adc@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture")
	return root
}

func checkoutWorktree(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	checkoutGit(t, root, "worktree", "add", "--detach", "-q", path, "HEAD")
	return path
}

func checkCheckoutSelection(t *testing.T, selected []shared.Project, want map[string]shared.Project) {
	t.Helper()
	if len(selected) != len(want) {
		t.Fatalf("selected %d checkouts, want %d: %+v", len(selected), len(want), selected)
	}
	seen := map[string]bool{}
	for _, p := range selected {
		expected, ok := want[p.RuntimePath]
		if !ok || seen[p.RuntimePath] || !reflect.DeepEqual(p, expected) {
			t.Fatalf("unexpected, repeated, or changed project: %+v; expected %+v", p, expected)
		}
		seen[p.RuntimePath] = true
	}
}

func TestAllSyncCheckoutsIncludesRegisteredWorktrees(t *testing.T) {
	first, second := checkoutRepository(t), checkoutRepository(t)
	firstTree := checkoutWorktree(t, first, "detached with spaces")
	secondTree := checkoutWorktree(t, second, "other")
	// An unrelated accessible repository is not part of registered scope.
	_ = checkoutRepository(t)
	projects := []shared.Project{
		{ID: "first", Path: "/host/first", RuntimePath: first, Capabilities: []string{"graphify"}},
		{ID: "second", Path: "/host/second", RuntimePath: second, ReadOnly: true},
	}
	want := map[string]shared.Project{first: projects[0], second: projects[1]}
	for index, path := range []string{firstTree, secondTree} {
		p := projects[index]
		p.RuntimePath, _ = shared.Abs(path)
		want[p.RuntimePath] = p
	}
	selected := allSyncCheckouts(projects)
	if !reflect.DeepEqual(selected[:len(projects)], projects) {
		t.Fatal("registered entries were changed or reordered")
	}
	checkCheckoutSelection(t, selected, want)
}

func TestAllSyncCheckoutsHonorsExplicitWorktreeRegistration(t *testing.T) {
	root := checkoutRepository(t)
	path := checkoutWorktree(t, root, "registered")
	projects := []shared.Project{
		{ID: "main", RuntimePath: root, Capabilities: []string{"graphify"}},
		{ID: "review", RuntimePath: path, ReadOnly: true, Capabilities: []string{"obsidian"}},
	}
	checkCheckoutSelection(t, allSyncCheckouts(projects), map[string]shared.Project{root: projects[0], path: projects[1]})
	// Registry order must not change either checkout's permissions or identity.
	projects[0], projects[1] = projects[1], projects[0]
	checkCheckoutSelection(t, allSyncCheckouts(projects), map[string]shared.Project{root: projects[1], path: projects[0]})
}

func TestAllSyncCheckoutsSkipsStaleAndForeignPaths(t *testing.T) {
	root := checkoutRepository(t)
	stale := checkoutWorktree(t, root, "missing")
	foreign := checkoutWorktree(t, root, "replaced")
	valid := checkoutWorktree(t, root, "valid")
	for _, path := range []string{stale, foreign} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(foreign, 0755); err != nil {
		t.Fatal(err)
	}
	checkoutGit(t, foreign, "init", "-q", "-b", "main")
	projects := []shared.Project{
		{ID: "repo", RuntimePath: root},
		{ID: "missing", RuntimePath: filepath.Join(t.TempDir(), "missing-project")},
		{ID: "damaged", RuntimePath: t.TempDir()},
	}
	want := map[string]shared.Project{}
	for _, p := range projects {
		want[p.RuntimePath] = p
	}
	worktree := projects[0]
	worktree.RuntimePath, _ = shared.Abs(valid)
	want[worktree.RuntimePath] = worktree
	checkCheckoutSelection(t, allSyncCheckouts(projects), want)
}

func TestAllSyncCheckoutsSkipsBareRepositoryEntry(t *testing.T) {
	root := checkoutRepository(t)
	bare := filepath.Join(t.TempDir(), "bare.git")
	checkoutGit(t, root, "clone", "--bare", "-q", root, bare)
	first := checkoutWorktree(t, bare, "first")
	second := checkoutWorktree(t, bare, "second")
	p := shared.Project{ID: "repo", RuntimePath: first}
	other := p
	other.RuntimePath, _ = shared.Abs(second)
	checkCheckoutSelection(t, allSyncCheckouts([]shared.Project{p}), map[string]shared.Project{first: p, other.RuntimePath: other})
}

func TestAllSyncCheckoutsPreservesUnusualPaths(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows forbids newlines in directory names")
	}
	root := checkoutRepository(t)
	path := checkoutWorktree(t, root, "line\nbreak\n")
	p := shared.Project{ID: "repo", RuntimePath: root}
	other := p
	other.RuntimePath, _ = shared.Abs(path)
	checkCheckoutSelection(t, allSyncCheckouts([]shared.Project{p}), map[string]shared.Project{root: p, other.RuntimePath: other})
}

func TestAllSyncCheckoutsDeduplicatesRegisteredAliases(t *testing.T) {
	root := checkoutRepository(t)
	path := checkoutWorktree(t, root, "registered")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	projects := []shared.Project{
		{ID: "repo", RuntimePath: root},
		{ID: "readonly", RuntimePath: alias, ReadOnly: true},
	}
	checkCheckoutSelection(t, allSyncCheckouts(projects), map[string]shared.Project{root: projects[0], alias: projects[1]})
}
