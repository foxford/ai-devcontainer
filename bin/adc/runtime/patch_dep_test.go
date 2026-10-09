package runtime

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestNpmRanges(t *testing.T) {
	for _, test := range []struct {
		version, rangeText string
		allowed, known     bool
	}{
		{"4.17.21", "^4.0.0", true, true}, {"5.1.16", "^3.3.18", false, true}, {"0.2.8", "^0.2.5", true, true}, {"0.3.0", "^0.2.5", false, true}, {"0.0.5", "^0.0.4", false, true}, {"0.0.9", "^0.0", true, true},
		{"1.2.9", "~1.2.3", true, true}, {"1.3.0", "~1.2.3", false, true}, {"1.9.0", "~1", true, true}, {"2.0.0", "~1", false, true},
		{"4.17.21", ">= 4.0.0 < 5", true, true}, {"4.17.21", "4.17.20", false, true}, {"4.17.21", "4.17.x", true, true}, {"4.17.21", "4", true, true}, {"4.17.21", "3 || >=4.17.0 <5", true, true},
		{"1.5.0", "1.2 - 2.3", true, true}, {"2.4.0", "1.2 - 2.3", false, true}, {"1.2.3", "1.2.3 - 1.2.3", true, true},
		{"1.2.3-beta.2", "^1.2.3-beta.1", true, true}, {"1.3.0-beta", "^1.2.3-beta.1", false, true}, {"1.2.3-beta", "*", false, true},
		{"1.2.3", ">*", false, true}, {"1.2.3", "workspace:*", false, false}, {"1.2.3", "npm:aliased@1", false, false},
	} {
		t.Run(test.version+" "+test.rangeText, func(t *testing.T) {
			allowed, known := rangeAllows(test.version, test.rangeText)
			if allowed != test.allowed || known != test.known {
				t.Fatalf("got %t %t; want %t %t", allowed, known, test.allowed, test.known)
			}
		})
	}
}

func TestDependencyHoldersScopePeerAndDeclarers(t *testing.T) {
	root := t.TempDir()
	for directory, data := range map[string]string{
		"old-lib@1.0.0":                `{"peerDependencies":{"nanoid":"5.1.15"}}`,
		"other-major@1.0.0":            `{"dependencies":{"nanoid":"^3.0.0"}}`,
		"allowed@1.0.0":                `{"dependencies":{"nanoid":"^5.0.0"}}`,
		"@scope+lib@1.0.0(peer@2.0.0)": `{"dependencies":{"nanoid":"5.1.15"}}`,
	} {
		m := pnpmStorePackage.FindStringSubmatch(directory)
		name := strings.ReplaceAll(m[1], "+", "/")
		writeFixture(t, filepath.Join(root, "node_modules", ".pnpm", directory, "node_modules", filepath.FromSlash(name), "package.json"), data)
	}
	writeFixture(t, filepath.Join(root, "packages", "app", "package.json"), `{"name":"@team/app","dependencies":{"old-lib":"1","@scope/lib":"1"}}`)
	writeFixture(t, filepath.Join(root, "node_modules", "noise", "package.json"), `{"name":"do-not-read","dependencies":{"old-lib":"1"}}`)
	holders, err := dependencyHolders(root, "nanoid", "5.1.16")
	if err != nil {
		t.Fatal(err)
	}
	if len(holders) != 2 || holders[0].Name != "@scope/lib" || holders[1].Name != "old-lib" || !reflect.DeepEqual(holders[1].Fields, []string{"peerDependencies"}) {
		t.Fatalf("wrong holders %#v", holders)
	}
	for _, holder := range holders {
		if !reflect.DeepEqual(holder.Declarers, []string{"@team/app"}) {
			t.Fatal("declarer scope", holder)
		}
	}
}

func TestPatchDependencyTransaction(t *testing.T) {
	for _, major := range []string{"10.5.0", "11.0.0"} {
		for _, mode := range []string{"sticky", "permanent", "fail-first", "fail-second", "fail-permanent"} {
			t.Run(major+" "+mode, func(t *testing.T) {
				root := t.TempDir()
				manifest := `{"name":"keep","pnpm":{"overrides":{"unrelated":"1.0.0"}}}` + "\n"
				workspace := "# team comment\npackages: ['packages/*']\ncatalog:\n  keep: 1.0.0\n"
				lock := "packages:\n  lodash@4.17.20: {}\n  lodash@3.0.0: {}\n"
				writeFixture(t, filepath.Join(root, "package.json"), manifest)
				writeFixture(t, filepath.Join(root, "pnpm-workspace.yaml"), workspace)
				writeFixture(t, filepath.Join(root, "pnpm-lock.yaml"), lock)
				calls := 0
				run := func(dir, name string, args ...string) (string, error) {
					if dir != root {
						t.Fatal("wrong command cwd")
					}
					if name == "pnpm" && len(args) == 1 && args[0] == "--version" {
						return major, nil
					}
					if name != "pnpm" || args[0] != "install" {
						return "", errors.New("unexpected command")
					}
					calls++
					if mode == "fail-first" && calls == 1 || mode == "fail-second" && calls == 2 || mode == "fail-permanent" && calls == 3 {
						writeFixture(t, filepath.Join(root, "pnpm-lock.yaml"), "corrupt on failure\n")
						return "fixture error", errors.New("failed install")
					}
					file := "package.json"
					if strings.HasPrefix(major, "11") {
						file = "pnpm-workspace.yaml"
					}
					text := readFixture(t, filepath.Join(root, file))
					has := strings.Contains(strings.ReplaceAll(strings.ReplaceAll(text, `\u003c`, "<"), `\u003e`, ">"), "lodash@>=4.0.0 <4.17.21")
					version := "4.17.20"
					if has || mode == "sticky" {
						version = "4.17.21"
					}
					writeFixture(t, filepath.Join(root, "pnpm-lock.yaml"), "packages:\n  lodash@"+version+": {}\n  lodash@3.0.0: {}\n")
					return "", nil
				}
				var out strings.Builder
				err := PatchDependency(root, "lodash", "4.17.21", run, &out)
				if strings.HasPrefix(mode, "fail-") {
					if err == nil {
						t.Fatal("failed install accepted")
					}
					for file, want := range map[string]string{"package.json": manifest, "pnpm-workspace.yaml": workspace, "pnpm-lock.yaml": lock} {
						if readFixture(t, filepath.Join(root, file)) != want {
							t.Fatal("rollback lost", file)
						}
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(readFixture(t, filepath.Join(root, "pnpm-lock.yaml")), "lodash@3.0.0") {
					t.Fatal("other major touched")
				}
				file := "package.json"
				if strings.HasPrefix(major, "11") {
					file = "pnpm-workspace.yaml"
				}
				text := readFixture(t, filepath.Join(root, file))
				if mode == "permanent" {
					if !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(text, `\u003c`, "<"), `\u003e`, ">"), "lodash@>=4.0.0 <4.17.21") {
						t.Fatal("permanent override missing")
					}
					if file == "pnpm-workspace.yaml" {
						var doc map[string]any
						if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
							t.Fatal(err)
						}
						if !strings.Contains(text, "# team comment") || doc["catalog"] == nil {
							t.Fatal("workspace comments or catalog lost")
						}
					}
				} else {
					want := manifest
					if file == "pnpm-workspace.yaml" {
						want = workspace
					}
					if text != want {
						t.Fatal("temporary override or formatting left behind")
					}
				}
			})
		}
	}
}

func TestPatchDependencyNoopAndVersionResolution(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "package.json"), "{}\n")
	writeFixture(t, filepath.Join(root, "pnpm-lock.yaml"), "packages:\n  lodash@4.17.21: {}\n")
	if err := PatchDependency(root, "lodash", "4.17.21", func(string, string, ...string) (string, error) {
		t.Fatal("no-op invoked a package manager")
		return "", nil
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	version, err := resolveVersion(root, "lodash", "4", func(_, _ string, _ ...string) (string, error) { return `["4.17.21","4.17.20"]`, nil })
	if err != nil || version != "4.17.21" {
		t.Fatal(version, err)
	}
	if err := PatchDependency(root, "--malicious", "4.17.21", nil, io.Discard); err == nil {
		t.Fatal("invalid package name accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "package.json.bak")); !os.IsNotExist(err) {
		t.Fatal("backup garbage left behind")
	}
}

func TestOverrideDoctorDoesNotDropUnknownRanges(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "package.json"), `{"pnpm":{"overrides":{"lodash@>=4.0.0 <4.17.21":"4.17.21"}}}`)
	writeFixture(t, filepath.Join(root, "node_modules", ".pnpm", "holder@1.0.0", "node_modules", "holder", "package.json"), `{"dependencies":{"lodash":"npm:aliased@4"}}`)
	var out strings.Builder
	if err := PatchDoctor(root, func(_, name string, args ...string) (string, error) {
		if name == "pnpm" {
			return "10.5.0", nil
		}
		return "", errors.New("registry unavailable")
	}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "override можно снять") || !strings.Contains(out.String(), "ручной проверки") {
		t.Fatal(out.String())
	}
}
