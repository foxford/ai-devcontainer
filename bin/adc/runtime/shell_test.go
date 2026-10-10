package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellDefaultsRestoreMigratedHomeAndPreserveSettings(t *testing.T) {
	seed, home := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(seed, ".oh-my-zsh", "oh-my-zsh.sh"), "framework")
	writeFixture(t, filepath.Join(seed, ".zshrc"), "# default theme\nsource $HOME/.oh-my-zsh/oh-my-zsh.sh\n")
	writeFixture(t, filepath.Join(seed, ".bashrc"), "# default bash\n")
	writeFixture(t, filepath.Join(home, ".zshrc"), "\n# adc:agents:start\n. /opt/ai-devcontainer/runtime/shell-env.sh\n# adc:agents:end\n")
	writeFixture(t, filepath.Join(home, ".codex", "auth.json"), "keep account")
	for range 2 {
		if err := initializeShells(home, seed); err != nil {
			t.Fatal(err)
		}
	}
	profile := readFixture(t, filepath.Join(home, ".zshrc"))
	if !strings.Contains(profile, "# default theme") || strings.Count(profile, "# adc:agents:start") != 1 || !strings.Contains(profile, "adc banner") {
		t.Fatal(profile)
	}
	if readFixture(t, filepath.Join(home, ".codex", "auth.json")) != "keep account" {
		t.Fatal("profile restoration modified agent account")
	}
	writeFixture(t, filepath.Join(home, ".zshrc"), "ZSH_THEME=personal\n# custom shell\n")
	writeFixture(t, filepath.Join(home, ".oh-my-zsh", "oh-my-zsh.sh"), "custom framework")
	if err := initializeShells(home, seed); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(readFixture(t, filepath.Join(home, ".zshrc")), "ZSH_THEME=personal\n# custom shell\n") || readFixture(t, filepath.Join(home, ".oh-my-zsh", "oh-my-zsh.sh")) != "custom framework" {
		t.Fatal("existing shell settings were overwritten")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".adc-zsh-") {
			t.Fatal("left shell staging directory")
		}
	}
}
