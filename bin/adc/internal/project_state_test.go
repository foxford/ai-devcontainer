package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectStateCopiesLegacyWithoutLosingOwnershipOrForks(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, LegacyProjectStateDir)
	for name, body := range map[string]string{"mcp-state": "old-server\n", "skills-base/example/SKILL.md": "fork base", "backups/orca.yaml": "original setup"} {
		file := filepath.Join(old, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := TreeDigest(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateProjectState(root); err != nil {
		t.Fatal(err)
	}
	after, err := TreeDigest(old)
	if err != nil || after != before {
		t.Fatal("original state changed")
	}
	for _, name := range []string{"mcp-state", "skills-base/example/SKILL.md", "backups/orca.yaml"} {
		want, _ := os.ReadFile(filepath.Join(old, filepath.FromSlash(name)))
		got, err := os.ReadFile(filepath.Join(root, ProjectStateDir, filepath.FromSlash(name)))
		if err != nil || string(got) != string(want) {
			t.Fatalf("lost %s", name)
		}
	}
	file := filepath.Join(root, ProjectStateDir, "mcp-state")
	if err := os.WriteFile(file, []byte("new ownership"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateProjectState(root); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); string(data) != "new ownership" {
		t.Fatal("second migration overwrote new state")
	}
}

func TestProjectStateRefusesAmbiguousDirectoriesAndLinks(t *testing.T) {
	for _, kind := range []string{"both directories", "legacy link", "new link", "nested link"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			old, next := filepath.Join(root, LegacyProjectStateDir), filepath.Join(root, ProjectStateDir)
			if err := os.Mkdir(old, 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "both directories":
				err = os.Mkdir(next, 0700)
			case "legacy link":
				if err := os.Remove(old); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(outside, old)
			case "new link":
				err = os.Symlink(outside, next)
			case "nested link":
				err = os.Symlink(outside, filepath.Join(old, "backups"))
			}
			if err != nil {
				t.Skipf("symlink privilege required: %v", err)
			}
			if err := MigrateProjectState(root); err == nil {
				t.Fatal("accepted unsafe state")
			}
			if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
				t.Fatal("wrote outside project")
			}
		})
	}
}
