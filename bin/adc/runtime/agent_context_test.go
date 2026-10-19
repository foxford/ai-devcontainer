package runtime

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A replacement only supplies its own layout; context delivery must not depend
// on a registered agent name or invoke any of its other lifecycle operations.
type contextOnlyDriver struct {
	agentDriver
	layout agentContextLayout
}

func (d contextOnlyDriver) Context() agentContextLayout { return d.layout }

func TestAgentContextAcceptsReplacementAndPreservesUserRules(t *testing.T) {
	workspace, home, project := t.TempDir(), t.TempDir(), t.TempDir()
	driver := contextOnlyDriver{layout: agentContextLayout{
		WorkspaceInstructions: []agentInstruction{{"REPLACEMENT.md", "workspace instructions\n"}},
		HomeInstructions:      []agentInstruction{{".replacement/rules.md", "home instructions"}},
		ProjectInstructions:   []agentInstruction{{"REPLACEMENT.md", "project instructions"}},
	}}
	homeRules := filepath.Join(home, ".replacement", "rules.md")
	projectRules := filepath.Join(project, "REPLACEMENT.md")
	writeFixture(t, homeRules, "personal home rules\n")
	writeFixture(t, projectRules, "personal project rules\n")
	writeFixture(t, filepath.Join(project, "AGENTS.md"), "shared project rules\n")
	for range 2 {
		if err := writeWorkspaceAgentContext(workspace, home, []agentDriver{driver}); err != nil {
			t.Fatal(err)
		}
		if err := writeProjectAgentContext(project, []agentDriver{driver}); err != nil {
			t.Fatal(err)
		}
	}
	for path, expected := range map[string][]string{
		filepath.Join(workspace, "REPLACEMENT.md"): {"workspace instructions"},
		homeRules:                           {"personal home rules", "home instructions"},
		projectRules:                        {"personal project rules", "project instructions"},
		filepath.Join(project, "AGENTS.md"): {"shared project rules", "AGENTS.platform.md"},
	} {
		content := readFixture(t, path)
		for _, text := range expected {
			if strings.Count(content, text) != 1 {
				t.Fatalf("%s: expected one %q, got %q", path, text, content)
			}
		}
	}
	for _, path := range []string{filepath.Join(home, ".codex"), filepath.Join(home, ".claude"), filepath.Join(workspace, "CLAUDE.md"), filepath.Join(project, "CLAUDE.md")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("unregistered agent context written: %s, %v", path, err)
		}
	}
}

func TestOpenCodeInstallerUsesVendorRetryRunner(t *testing.T) {
	i := toolInstaller{home: t.TempDir(), tools: t.TempDir(), out: io.Discard}
	checks, vendors := 0, 0
	i.fetch = func(string) ([]byte, error) { return []byte("vendor installer"), nil }
	i.run = func(_ string, name string, args ...string) ([]byte, error) {
		if name != "opencode" || len(args) != 1 || args[0] != "--version" {
			t.Fatalf("vendor bypassed retry runner: %s %v", name, args)
		}
		checks++
		if checks == 1 {
			return nil, errors.New("not installed")
		}
		return []byte("fixture version"), nil
	}
	i.runVendor = func(_ string, name string, args ...string) ([]byte, error) {
		vendors++
		if name != "bash" || len(args) != 1 || readFixture(t, args[0]) != "vendor installer" {
			t.Fatalf("unexpected vendor invocation: %s %v", name, args)
		}
		return nil, nil
	}
	if err := (openCodeDriver{}).Install(&i); err != nil {
		t.Fatal(err)
	}
	if checks != 2 || vendors != 1 {
		t.Fatalf("verification/vendor counts: %d/%d", checks, vendors)
	}
}
