package host

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
