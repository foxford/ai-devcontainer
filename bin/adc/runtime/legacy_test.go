package runtime

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/legacy"
)

func legacyFixture(t *testing.T) (string, legacy.Backup) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("runtime imports and ownership run in a Linux container")
	}
	dir := t.TempDir()
	b := legacy.Backup{Version: 1, Created: "fixture", Plan: legacy.Plan{Version: 1}, Archives: map[string]legacy.Archive{}}
	sources := map[string]string{}
	for _, id := range []string{"app-a", "app-b"} {
		c := legacy.Container{ID: id, Name: id, Project: id, ProjectPath: "/projects/" + id, RuntimePath: "/workspaces/" + id}
		for _, agent := range []string{"codex", "claude", "hermes", "history", "obsidian-vault"} {
			resource := id + "-" + agent
			source := t.TempDir()
			sources[resource] = source
			b.Plan.Resources = append(b.Plan.Resources, legacy.Resource{ID: resource, Type: "bind", Source: source})
			destination := "/home/node/." + agent
			if agent == "obsidian-vault" {
				destination = "/home/node/obsidian-vault"
			}
			c.Mounts = append(c.Mounts, legacy.Mount{Type: "bind", Source: source, Destination: destination, Resource: resource})
			switch agent {
			case "codex":
				writeFixture(t, filepath.Join(source, "auth.json"), id+"-auth")
				writeFixture(t, filepath.Join(source, "sessions", "same.jsonl"), id+"-session")
				writeFixture(t, filepath.Join(source, "sessions", id+".jsonl"), id+"-unique-session")
				writeFixture(t, filepath.Join(source, "state.sqlite"), id+"-database")
				writeFixture(t, filepath.Join(source, "skills", "personal", "SKILL.md"), id+"-personal-skill")
			case "claude":
				writeFixture(t, filepath.Join(source, "settings.json"), id+"-settings")
				writeFixture(t, filepath.Join(source, "projects", id, "chat.jsonl"), id+"-claude-chat")
			case "hermes":
				if err := os.Symlink("/opt/ai-tools/share/hermes/auth.json", filepath.Join(source, "auth.json")); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(source, "config.yaml"), "model: "+id+"\n")
			case "history":
				writeFixture(t, filepath.Join(source, ".zsh_history"), id+"-command\n")
			case "obsidian-vault":
				writeFixture(t, filepath.Join(source, "notes", "example.md"), id+"-note")
			}
		}
		c.Mounts = append(c.Mounts, legacy.Mount{Type: "volume", Source: "fixture-tools", Destination: "/opt/ai-tools", Resource: "common-tools"})
		b.Plan.Containers = append(b.Plan.Containers, c)
		root := t.TempDir()
		writeFixture(t, filepath.Join(root, "home/node/.local/share/opencode/auth.json"), id+"-opencode-auth")
		sources["container-"+id] = root
	}
	tools := t.TempDir()
	writeFixture(t, filepath.Join(tools, "share/hermes/auth.json"), "shared-hermes-auth")
	writeFixture(t, filepath.Join(tools, "share/mcp/servers.json"), "{\"mcpServers\":{}}")
	writeFixture(t, filepath.Join(tools, "share/mcp/secrets.env"), "FAKE_KEY=fixture\n")
	sources["common-tools"] = tools
	b.Plan.Resources = append(b.Plan.Resources, legacy.Resource{ID: "common-tools", Type: "volume", Source: "fixture-tools"})
	for key, source := range sources {
		var buf bytes.Buffer
		if err := LegacyArchive(source, &buf); err != nil {
			t.Fatal(err)
		}
		setLegacyArchive(t, dir, &b, key, buf.Bytes())
	}
	for _, c := range b.Plan.Containers {
		setLegacyArchive(t, dir, &b, "metadata-"+c.ID, []byte("{}"))
	}
	saveLegacyManifest(t, dir, b)
	return dir, b
}

func setLegacyArchive(t *testing.T, dir string, b *legacy.Backup, key string, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	name := key + ".tar"
	if strings.HasPrefix(key, "metadata-") {
		name = key + ".json"
	}
	b.Archives[key] = legacy.Archive{File: name, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func saveLegacyManifest(t *testing.T, dir string, b legacy.Backup) {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "backup.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyImportPreservesBothProfilesAndResolvesHermes(t *testing.T) {
	dir, b := legacyFixture(t)
	home, tools := t.TempDir(), t.TempDir()
	if err := ImportLegacy(dir, "app-b", home, tools, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{
		".adc-legacy/app-a/home/.codex/skills/personal/SKILL.md": "app-a-personal-skill",
		".history/.zsh_history":                                  "app-b-command\napp-a-command\n",
		"obsidian-vault/notes/example.md":                        "app-b-note",
		".adc-legacy/app-a/home/obsidian-vault/notes/example.md": "app-a-note",
		".codex/auth.json":                                       "app-b-auth",
		".codex/sessions/same.jsonl":                             "app-b-session",
		".codex/sessions/app-a.jsonl":                            "app-a-unique-session",
		".codex/state.sqlite":                                    "app-b-database",
		".claude/settings.json":                                  "app-b-settings",
		".claude/projects/app-a/chat.jsonl":                      "app-a-claude-chat",
		".hermes/auth.json":                                      "shared-hermes-auth",
		".local/share/opencode/auth.json":                        "app-b-opencode-auth",
		".adc-legacy/app-a/home/.codex/sessions/same.jsonl":      "app-a-session",
		".adc-legacy/app-a/home/.codex/auth.json":                "app-a-auth",
		".adc-legacy/app-a/home/.codex/state.sqlite":             "app-a-database",
		".adc-legacy/app-a/home/.hermes/auth.json":               "shared-hermes-auth",
	} {
		if got := readFixture(t, filepath.Join(home, filepath.FromSlash(name))); got != expected {
			t.Fatalf("%s: %q != %q", name, got, expected)
		}
		info, err := os.Stat(filepath.Join(home, filepath.FromSlash(name)))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private permissions lost: %s %v", name, err)
		}
	}
	if got := readFixture(t, filepath.Join(tools, "share/mcp/secrets.env")); got != "FAKE_KEY=fixture\n" {
		t.Fatal("shared MCP secrets not copied")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "personal", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("legacy skill bypassed new delivery tree")
	}
	var report legacyImportReport
	if err := legacy.ReadJSON(filepath.Join(home, ".adc-legacy/report.json"), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Conflicts) != 1 || report.Account != "app-b" || !report.Complete {
		t.Fatalf("collision not reported: %+v", report)
	}
	if err := VerifyLegacyImport(home, legacy.Fingerprint(b), "app-b"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyLegacyImport(home, legacy.Fingerprint(b), "app-a"); err == nil {
		t.Fatal("verified wrong account")
	}
	report.Complete = false
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, ".adc-legacy/report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = VerifyLegacyImport(home, legacy.Fingerprint(b), "app-b"); err == nil {
		t.Fatal("verified interrupted publication")
	}
	// Every source archive and its checksum still exist after import.
	if _, err := legacy.Verify(dir); err != nil {
		t.Fatal("import changed backup:", err)
	}
	for _, resource := range b.Plan.Resources {
		if resource.Type == "bind" && strings.HasSuffix(resource.ID, "codex") {
			if got := readFixture(t, filepath.Join(resource.Source, "auth.json")); !strings.HasPrefix(got, "app-") {
				t.Fatal("original changed")
			}
		}
	}
	if err := ImportLegacy(dir, "app-a", home, tools, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("overwrote existing target")
	}
	if got := readFixture(t, filepath.Join(home, ".codex/auth.json")); got != "app-b-auth" {
		t.Fatal("retry changed live account")
	}
}

func TestLegacyImportRejectsBadBackupWithoutPublishing(t *testing.T) {
	for _, kind := range []string{"incomplete", "checksum", "traversal", "broken-hermes", "truncated-tar", "auth-link"} {
		t.Run(kind, func(t *testing.T) {
			dir, b := legacyFixture(t)
			switch kind {
			case "incomplete":
				if err := os.Remove(filepath.Join(dir, b.Archives["common-tools"].File)); err != nil {
					t.Fatal(err)
				}
			case "checksum":
				if err := os.WriteFile(filepath.Join(dir, b.Archives["common-tools"].File), []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "broken-hermes":
				var buf bytes.Buffer
				tw := tar.NewWriter(&buf)
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				setLegacyArchive(t, dir, &b, "common-tools", buf.Bytes())
			case "truncated-tar":
				setLegacyArchive(t, dir, &b, "app-a-codex", []byte("truncated tar"))
			default:
				var buf bytes.Buffer
				tw := tar.NewWriter(&buf)
				h := &tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Size: 3, Mode: 0600}
				if kind == "auth-link" {
					h = &tar.Header{Name: "auth.json", Typeflag: tar.TypeSymlink, Linkname: "/outside/auth.json", Mode: 0600}
				}
				if err := tw.WriteHeader(h); err != nil {
					t.Fatal(err)
				}
				if kind == "traversal" {
					if _, err := tw.Write([]byte("bad")); err != nil {
						t.Fatal(err)
					}
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				setLegacyArchive(t, dir, &b, "app-a-codex", buf.Bytes())
			}
			saveLegacyManifest(t, dir, b)
			home, tools := t.TempDir(), t.TempDir()
			if err := ImportLegacy(dir, "app-a", home, tools, os.Getuid(), os.Getgid()); err == nil {
				t.Fatal("accepted unsafe backup")
			}
			for _, root := range []string{home, tools} {
				if err := emptyLegacyDestination(root); err != nil {
					t.Fatal("published partial target:", err)
				}
			}
		})
	}
}

func TestLegacyArchiveDoesNotFollowAbsoluteSymlinks(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Unix symlinks")
	}
	root, outside := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(outside, "secret"), "not-in-archive")
	if err := os.Symlink(outside, filepath.Join(root, "foreign")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := LegacyArchive(root, &buf); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&buf)
	seen := false
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if h.Name == "foreign" {
			seen = true
			if h.Typeflag != tar.TypeSymlink || h.Linkname != outside {
				t.Fatal("link changed")
			}
		}
		if strings.Contains(h.Name, "secret") {
			t.Fatal("followed foreign symlink")
		}
	}
	if !seen {
		t.Fatal("link metadata not archived")
	}
}

func TestLegacyArchiveAndImportDoNotDeleteSource(t *testing.T) {
	// AtomicWrite itself may replace generated files elsewhere, but the legacy
	// importer only writes into new staging roots and never uses source paths.
	dir, _ := legacyFixture(t)
	home, tools := t.TempDir(), t.TempDir()
	before, err := os.ReadFile(filepath.Join(dir, "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = ImportLegacy(dir, "app-a", home, tools, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if after := readFixture(t, filepath.Join(dir, "backup.json")); after != string(before) {
		t.Fatal("source manifest changed")
	}
	if err = shared.AtomicWrite(filepath.Join(home, ".codex/auth.json"), []byte("changed-live-account"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Verify(dir); err != nil {
		t.Fatal("live changes affected archive")
	}
}

func TestLegacyTarDetectsChangeWhileReading(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Linux runtime tar reader")
	}
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "value"), "original")
	var buf bytes.Buffer
	if err := LegacyArchive(root, &buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	file := filepath.Join(t.TempDir(), "archive.tar")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	changed := false
	err := readLegacyTar(file, hex.EncodeToString(sum[:]), func(_ *tar.Header, _ string, _ io.Reader) error {
		if changed {
			return nil
		}
		changed = true
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err = f.Write([]byte("changed-after-verification")); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	})
	if err == nil || !strings.Contains(err.Error(), "изменился") {
		t.Fatalf("accepted archive changed during read: %v", err)
	}
}
