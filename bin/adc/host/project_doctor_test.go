package host

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestProjectDoctorChecksWithoutChangingLegacyProject(t *testing.T) {
	a, root := cleanupFixture(t)
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	before, err := legacyPlatformDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.DoctorProject(root, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".devcontainer", ".ai-devcontainer", "AGENTS.platform.md", "файлы не изменены"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s: %s", want, out.String())
		}
	}
	after, err := legacyPlatformDigest(root)
	if err != nil || after != before {
		t.Fatal("doctor modified checkout")
	}
	if _, err := os.Lstat(a.State); !os.IsNotExist(err) {
		t.Fatal("doctor created state")
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, ".agents", "broken")); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	out.Reset()
	if err := a.DoctorProject(root, &out); err == nil || !strings.Contains(out.String(), "неисправная ссылка") {
		t.Fatal("doctor missed broken link")
	}
}

func TestProjectDoctorRecognizesCurrentDirectory(t *testing.T) {
	a, root := cleanupFixture(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := a.shared().Save(shared.Config{Version: shared.Schema, Name: "developer", Port: 6768, Projects: []shared.Project{{ID: "registered", Path: root, RuntimePath: root}}}); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	var out bytes.Buffer
	if err := a.DoctorProject(".", &out); err != nil || !strings.Contains(out.String(), "OK: подключён как registered") || strings.Contains(out.String(), "ещё не подключён") {
		t.Fatal(err, out.String())
	}
}

func TestProjectDoctorResolvesContainerLinkChainsWithoutNoise(t *testing.T) {
	a, root := cleanupFixture(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	old := "/workspaces/" + filepath.Base(root)
	links := map[string]string{
		".agents/tools/venv/bin/python":     "/home/node/.local/share/uv/python/bin/python3.12",
		".agents/tools/venv/bin/python3":    "python",
		".agents/tools/venv/bin/python3.12": "python3",
		".agents/tools/home":                "/home/node",
		".agents/tools/interpreter":         "home/.local/share/uv/python/bin/python3.12",
		".claude/skills/stock/SKILL.md":     "/opt/ai-devcontainer/skills/stock/SKILL.md",
		".claude/skills/fork/SKILL.md":      old + "/.agents/skills/stock/SKILL.md",
		".dsh/skills/stock":                 old + "/.claude/skills/stock",
	}
	for i := 0; i < 200; i++ {
		links[fmt.Sprintf(".claude/skills/stock/references/%03d.md", i)] = fmt.Sprintf("/opt/ai-devcontainer/skills/stock/references/%03d.md", i)
	}
	for rel, target := range links {
		file := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, file); err != nil {
			t.Skipf("symlink privilege required: %v", err)
		}
	}
	before, err := legacyPlatformDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.DoctorProject(root, &out); err != nil {
		t.Fatal(err)
	}
	if text := out.String(); strings.Contains(text, "ERROR:") || !strings.Contains(text, "ссылок с контейнерными путями: 208") || strings.Count(text, "\n") > 15 {
		t.Fatalf("noisy or incorrect report: %s", text)
	}
	if after, err := legacyPlatformDigest(root); err != nil || after != before {
		t.Fatal("doctor modified links or original files")
	}
	// A genuinely missing target in the mapped checkout must still fail.
	if err := os.Symlink(old+"/.agents/missing", filepath.Join(root, ".agents", "missing-link")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = a.DoctorProject(root, &out)
	if err == nil || !strings.Contains(out.String(), "missing-link") {
		t.Fatal("masked missing checkout target")
	}
	if strings.Contains(err.Error(), "missing-link") {
		t.Fatal("duplicated detailed error in returned CLI summary")
	}
}

func TestProjectLinkCyclesAndRuntimeTargetsRemainErrors(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	if err := os.Symlink("b", a); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	if err := os.Symlink("a", b); err != nil {
		t.Fatal(err)
	}
	if _, err := checkProjectLink(a, nil, true); err == nil {
		t.Fatal("missed symlink cycle")
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	missing := "/workspaces/adc-missing-" + filepath.Base(root) + "/target"
	if err := os.Symlink(missing, a); err != nil {
		t.Fatal(err)
	}
	if _, err := checkProjectLink(a, nil, false); err == nil {
		t.Fatal("runtime incorrectly skipped unavailable target")
	}
	if container, err := checkProjectLink(a, nil, true); err != nil || !container {
		t.Fatal("host treated an unknown runtime path as broken")
	}
	if _, err := checkProjectLink(missing, nil, true); err == nil {
		t.Fatal("skipped the original host path before reading a link")
	}
}
