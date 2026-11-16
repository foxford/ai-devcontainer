package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestSyncRepairsLegacyContractLinks(t *testing.T) {
	requireSymlinks(t)
	for _, relative := range []bool{false, true} {
		for _, available := range []bool{false, true} {
			name := "absolute"
			if relative {
				name = "relative"
			}
			if available {
				name += "/available"
			} else {
				name += "/missing"
			}
			t.Run(name, func(t *testing.T) {
				a, p, _ := mcpFixture(t)
				t.Setenv("ADC_INSTALL_AI_TOOLS", "0")
				source := filepath.Join(a.Platform, "docs", "AGENTS.platform.md")
				const legacy = "# Legacy platform instructions without a generated marker\n"
				if available {
					writeFixture(t, source, legacy)
				}
				target := filepath.Join(p.RuntimePath, "AGENTS.platform.md")
				link := source
				if relative {
					var err error
					link, err = filepath.Rel(p.RuntimePath, source)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(link, target); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(p.RuntimePath, "AGENTS.md"), "Project-specific rules\n")
				writeFixture(t, filepath.Join(p.RuntimePath, "CLAUDE.md"), "Team Claude rules\n")
				writeFixture(t, filepath.Join(p.RuntimePath, shared.LegacyProjectStateDir, "platform-rev"), "old-revision\n")
				for range 2 {
					if err := a.SyncProject(p); err != nil {
						t.Fatal(err)
					}
					info, err := os.Lstat(target)
					if err != nil || !info.Mode().IsRegular() || !strings.HasPrefix(readFixture(t, target), shared.Marker) {
						t.Fatalf("contract was not delivered as a generated regular file: %v", err)
					}
					for file, want := range map[string]string{
						"AGENTS.md":  "Project-specific rules",
						"CLAUDE.md":  "Team Claude rules",
						"orca.yaml":  "adc sync",
						".gitignore": "/AGENTS.platform.md",
					} {
						if !strings.Contains(readFixture(t, filepath.Join(p.RuntimePath, file)), want) {
							t.Fatalf("missing project text or incomplete delivery: %s", file)
						}
					}
					if available && readFixture(t, source) != legacy {
						t.Fatal("legacy source was modified")
					}
				}
			})
		}
	}
}

func TestSyncPreservesForeignContract(t *testing.T) {
	requireSymlinks(t)
	for _, kind := range []string{"file", "available link", "missing link"} {
		t.Run(kind, func(t *testing.T) {
			a, p, _ := mcpFixture(t)
			t.Setenv("ADC_INSTALL_AI_TOOLS", "0")
			target := filepath.Join(p.RuntimePath, "AGENTS.platform.md")
			source := filepath.Join(t.TempDir(), "docs", "AGENTS.platform.md")
			const rules = "Team-owned platform rules\n"
			if kind == "file" {
				writeFixture(t, target, rules)
			} else {
				if kind == "available link" {
					writeFixture(t, source, rules)
				}
				if err := os.Symlink(source, target); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.SyncProject(p); err == nil || !strings.Contains(err.Error(), "AGENTS.platform.md принадлежит проекту") {
				t.Fatalf("expected ownership conflict, got %v", err)
			}
			if kind == "file" {
				if readFixture(t, target) != rules {
					t.Fatal("project contract was modified")
				}
			} else if link, err := os.Readlink(target); err != nil || link != source {
				t.Fatalf("foreign link was modified: %q %v", link, err)
			}
		})
	}
}

func TestDeliveryRepairsLegacySecretsExample(t *testing.T) {
	requireSymlinks(t)
	for _, relative := range []bool{false, true} {
		a, p, _ := mcpFixture(t)
		target := filepath.Join(p.RuntimePath, ".agents", "mcp.secrets.env.example")
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(a.Platform, "docs", "mcp-secrets.env.example")
		if relative {
			var err error
			link, err = filepath.Rel(filepath.Dir(target), link)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(link, target); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := a.DeliverDocuments(p); err != nil {
				t.Fatal(err)
			}
			if readFixture(t, target) != readFixture(t, a.shared().DeliveryPath("mcp/secrets.env.example")) {
				t.Fatal("legacy secrets example was not repaired")
			}
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(t.TempDir(), "docs", "mcp-secrets.env.example")
		if err := os.Symlink(foreign, target); err != nil {
			t.Fatal(err)
		}
		if err := a.DeliverDocuments(p); err != nil {
			t.Fatal(err)
		}
		if link, err := os.Readlink(target); err != nil || link != foreign {
			t.Fatal("foreign secrets example link was modified", link, err)
		}
	}
}
