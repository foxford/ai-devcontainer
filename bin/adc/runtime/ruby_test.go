package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRubyPluginRestoresPopulatedHomeWithoutChangingVersions(t *testing.T) {
	home, seed := t.TempDir(), t.TempDir()
	install := filepath.Join(seed, "bin", "install")
	writeFixture(t, install, "#!/usr/bin/env bash\nexit 0\n")
	if err := os.Chmod(install, 0755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(seed, ".git", "config"), "plugin remote\n")
	// Existing home volumes can already contain interpreters, other plugins and
	// personal selections. Seeding must not reset any of them.
	for _, path := range []string{".tool-versions", ".asdfrc", ".asdf/plugins/nodejs/bin/install", ".asdf/installs/ruby/3.4.13/bin/ruby", ".asdf/shims/ruby", ".codex/auth.json"} {
		writeFixture(t, filepath.Join(home, path), "keep "+path)
	}
	if err := initializeRuby(home, seed); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(home, ".asdf", "plugins", "ruby")
	if readFixture(t, filepath.Join(plugin, ".git", "config")) != "plugin remote\n" {
		t.Fatal("lost plugin Git metadata needed for explicit updates")
	}
	info, err := os.Stat(filepath.Join(plugin, "bin", "install"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0755 {
		t.Fatalf("lost plugin executable permissions: %v", info.Mode())
	}
	writeFixture(t, filepath.Join(plugin, "bin", "install"), "user updated plugin")
	// A later ADC image can offer a new seed; it must not replace user updates.
	writeFixture(t, install, "new image plugin")
	if err := initializeRuby(home, seed); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, filepath.Join(plugin, "bin", "install")) != "user updated plugin" {
		t.Fatal("replaced an existing plugin")
	}
	for _, path := range []string{".tool-versions", ".asdfrc", ".asdf/plugins/nodejs/bin/install", ".asdf/installs/ruby/3.4.13/bin/ruby", ".asdf/shims/ruby", ".codex/auth.json"} {
		if readFixture(t, filepath.Join(home, path)) != "keep "+path {
			t.Fatalf("modified developer state: %s", path)
		}
	}
	assertNoRubyStaging(t, home)
}

func TestRubyPluginSeedFailureIsRetryable(t *testing.T) {
	home := t.TempDir()
	seed := filepath.Join(t.TempDir(), "missing")
	if err := initializeRuby(home, seed); err == nil {
		t.Fatal("missing image seed accepted")
	}
	plugin := filepath.Join(home, ".asdf", "plugins", "ruby")
	if _, err := os.Lstat(plugin); !os.IsNotExist(err) {
		t.Fatalf("failed preparation published a partial plugin: %v", err)
	}
	assertNoRubyStaging(t, home)
	writeFixture(t, filepath.Join(seed, "bin", "install"), "plugin")
	if err := initializeRuby(home, seed); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, filepath.Join(plugin, "bin", "install")) != "plugin" {
		t.Fatal("retry failed to prepare plugin")
	}
}

func TestRubyPluginPreservesUserPaths(t *testing.T) {
	t.Run("file collision", func(t *testing.T) {
		home := t.TempDir()
		plugin := filepath.Join(home, ".asdf", "plugins", "ruby")
		writeFixture(t, plugin, "user file")
		if err := initializeRuby(home, "missing-seed"); err == nil {
			t.Fatal("non-directory plugin accepted")
		}
		if readFixture(t, plugin) != "user file" {
			t.Fatal("replaced conflicting user file")
		}
	})
	t.Run("custom linked plugin", func(t *testing.T) {
		requireSymlinks(t)
		home, custom := t.TempDir(), t.TempDir()
		plugin := filepath.Join(home, ".asdf", "plugins", "ruby")
		if err := os.MkdirAll(filepath.Dir(plugin), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(custom, plugin); err != nil {
			t.Fatal(err)
		}
		if err := initializeRuby(home, "missing-seed"); err != nil {
			t.Fatal(err)
		}
		if got, err := os.Readlink(plugin); err != nil || got != custom {
			t.Fatalf("replaced linked plugin: %s %v", got, err)
		}
	})
}

func assertNoRubyStaging(t *testing.T, home string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, ".asdf", "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".adc-ruby-") {
			t.Fatalf("left partial plugin: %s", entry.Name())
		}
	}
}
