package runtime

import (
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthImportAndExactRestore(t *testing.T) {
	source, home, backups := t.TempDir(), t.TempDir(), t.TempDir()
	if e := shared.AtomicWrite(filepath.Join(source, "codex", "auth.json"), []byte("new-auth"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := shared.AtomicWrite(filepath.Join(source, ".hermes", ".env"), []byte("test-provider-key"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := shared.AtomicWrite(filepath.Join(home, ".codex", "auth.json"), []byte("previous-auth"), 0600); e != nil {
		t.Fatal(e)
	}
	backup, e := importAuth(source, home, backups)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if string(data) != "new-auth" {
		t.Fatal("auth not imported")
	}
	if e = restoreAuth(backup, home); e != nil {
		t.Fatal(e)
	}
	data, _ = os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if string(data) != "previous-auth" {
		t.Fatal("old auth not restored")
	}
	if _, e = os.Stat(filepath.Join(home, ".hermes", ".env")); !os.IsNotExist(e) {
		t.Fatal("previously absent auth not removed on restore")
	}
}
