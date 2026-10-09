package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Home(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ADC_HOME", filepath.Join(home, ".adc"))
	t.Setenv("ADC_RUNTIME", "0")
	t.Setenv("ZDOTDIR", home)
	return home
}

func Root(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("platform root not found")
		}
		dir = parent
	}
}

func Delivery(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(Root(t), "delivery")
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, "delivery", rel)
		if entry.IsDir() {
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
			if rel == "skills" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(rel, "skills"+string(os.PathSeparator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
