package runtime

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func TestToolInstallerUsesRealPATH(t *testing.T) {
	root := t.TempDir()
	home, tools := filepath.Join(root, "home"), filepath.Join(root, "tools")
	adapter := filepath.Join(root, "adc-agents")
	t.Setenv("PATH", adapter)
	env := installerEnv(home, tools)
	path := ""
	for _, item := range env {
		if value, ok := strings.CutPrefix(item, "PATH="); ok {
			path = value
		}
	}
	entries := filepath.SplitList(path)
	if len(entries) == 0 || entries[0] != filepath.Join(tools, "bin") || strings.Contains(path, "adc-agents") {
		t.Fatal("adapter can be mistaken for a real CLI:", path)
	}
	newEnv := replaceEnv([]string{"A=old", "B=keep", "A=duplicate"}, map[string]string{"A": "new"})
	if strings.Join(newEnv, "\n") != "B=keep\nA=new" {
		t.Fatal(newEnv)
	}
}

func TestToolInstallerRepairsBrokenAgentsAndPinsOmniRoute(t *testing.T) {
	for _, optionalFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "all available", true: "optional failures"}[optionalFails], func(t *testing.T) {
			home, tools := t.TempDir(), t.TempDir()
			platform := testutil.Root(t)
			installed := map[string]bool{"claude": false, "opencode": true, "hermes": false, "uv": true, "codex": true, "dsh": false, "graphify": false, "strix": false, "skaffold": true, "omniroute": false}
			writeFixture(t, filepath.Join(home, ".hermes", "hermes-agent", "broken"), "stale venv")
			writeFixture(t, filepath.Join(tools, "bin", "claude"), "broken launcher")
			fetches := []string{}
			calls := []string{}
			version := false
			i := toolInstaller{home: home, tools: tools, platform: platform, out: io.Discard, graphify: true}
			i.fetch = func(url string) ([]byte, error) { fetches = append(fetches, url); return []byte(url), nil }
			i.run = func(dir, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
					if !installed[name] {
						return nil, errors.New("broken or missing")
					}
					if name == "omniroute" {
						return []byte("3.8.51\n"), nil
					}
					if name == "graphify" {
						return []byte("graphify 0.9.82\n"), nil
					}
					return []byte("tool 0.9.82\n"), nil
				}
				if name == "bash" {
					data, err := os.ReadFile(args[0])
					if err != nil {
						t.Fatal(err)
					}
					agent := "claude"
					if strings.Contains(string(data), "hermes-agent") {
						agent = "hermes"
						if _, err := os.Stat(filepath.Join(home, ".hermes", "hermes-agent", "broken")); !os.IsNotExist(err) {
							t.Fatal("broken Hermes venv not removed")
						}
					}
					installed[agent] = true
					writeFixture(t, filepath.Join(home, ".local", "bin", agent), "real launcher")
					return nil, nil
				}
				if name == "uv" && len(args) >= 3 && args[1] == "install" {
					pkg := args[len(args)-1]
					if pkg == "graphifyy==0.9.82" {
						installed["graphify"] = true
					} else {
						if optionalFails {
							return nil, errors.New("Strix unavailable")
						}
						installed["strix"] = true
					}
					return nil, nil
				}
				if name == "npm" {
					joined := strings.Join(args, " ")
					if strings.Contains(joined, "@deepseek-ai/dsh") {
						if optionalFails {
							return nil, errors.New("DSH unavailable")
						}
						installed["dsh"] = true
					}
					if strings.Contains(joined, "omniroute@") {
						if !strings.Contains(joined, "omniroute@3.8.51") || !strings.Contains(joined, "--allow-scripts=omniroute,") {
							t.Fatal("OmniRoute version or lifecycle scripts not pinned", args)
						}
						installed["omniroute"] = true
						version = true
					}
					return nil, nil
				}
				return nil, nil
			}
			if err := i.install(); err != nil {
				t.Fatal(err)
			}
			if len(fetches) != 2 || !version {
				t.Fatalf("vendor installers %v; OmniRoute installed=%v", fetches, version)
			}
			if readFixture(t, filepath.Join(tools, "bin", "claude")) != "real launcher" {
				t.Fatal("agent or plugin not delivered")
			}
			if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "skills")); !os.IsNotExist(err) {
				t.Fatal("installer delivered private stock skills outside catalog")
			}
			before := len(fetches)
			if err := i.install(); err != nil {
				t.Fatal(err)
			}
			if len(fetches) != before {
				t.Fatal("healthy agent reinstalled")
			}
		})
	}
}

func TestToolPromotionPreservesPersistentSymlink(t *testing.T) {
	requireSymlinks(t)
	home, tools := t.TempDir(), t.TempDir()
	binary := filepath.Join(home, "versions", "cli")
	writeFixture(t, binary, "real binary")
	source := filepath.Join(home, ".local", "bin", "cli")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, source); err != nil {
		t.Fatal(err)
	}
	i := toolInstaller{home: home, tools: tools}
	if err := i.promote("cli", source); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(filepath.Join(tools, "bin", "cli")); err != nil || link != binary {
		t.Fatalf("symlink lost: %s %v", link, err)
	}
}
