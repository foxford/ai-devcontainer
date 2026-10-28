package host

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func workspaceConfigFixture(t *testing.T) (*App, shared.Config) {
	t.Helper()
	a := &App{State: t.TempDir()}
	c := shared.Config{Version: shared.Schema, Name: "existing", Port: 6768, OmniRoutePort: 21128, GraphPort: 4342,
		USBDevices: []string{"2104:0313"},
		Projects: []shared.Project{{ID: "app", Path: filepath.Join(t.TempDir(), "repo"), RuntimePath: "/projects/app", ReadOnly: true,
			Description: "existing project", Capabilities: []string{"graphify"}, Related: []string{"other"},
			Knowledge: []shared.Knowledge{{Path: "/knowledge/project", Origin: "team", Updated: "2026-10-15"}}}},
		Knowledge: []shared.Knowledge{{Path: "/knowledge/global", Origin: "global", Updated: "2026-10-15"}}}
	if err := a.shared().Save(c); err != nil {
		t.Fatal(err)
	}
	return a, c
}

func TestWorkspaceInitUsesHostPortDefault(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"init"}, 6770},
		{[]string{"init", "custom", "--port", "6780"}, 6780},
		{[]string{"init", "legacy", "--port", "6768"}, 6768},
	} {
		a := &App{State: t.TempDir()}
		if err := a.Workspace(tc.args); err != nil {
			t.Fatal(err)
		}
		c, err := a.shared().Load()
		if err != nil || c.Port != tc.want {
			t.Fatalf("args=%v port=%d err=%v", tc.args, c.Port, err)
		}
	}
}

func TestWorkspaceConfigReadsSavedPortsWithoutWritingOrDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a, c := workspaceConfigFixture(t)
	// Zero values in older configs mean the established gateway/graph defaults.
	c.OmniRoutePort, c.GraphPort = 0, 0
	if err := a.shared().Save(c); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(a.State, "workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.WorkspaceConfig(nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Сохранённые порты", "127.0.0.1:6768", "127.0.0.1:20128", "127.0.0.1:4242", "Выбранные USB (VID:PID): 2104:0313"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	after, err := os.ReadFile(filepath.Join(a.State, "workspace.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reading changed saved configuration", err)
	}
	entries, err := os.ReadDir(a.State)
	if err != nil || len(entries) != 1 {
		t.Fatal("reading created state files", entries, err)
	}
}

func TestWorkspaceConfigChangesOnlyRequestedPortsWithoutDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct {
		name string
		args []string
		want [3]int
	}{
		{"orca", []string{"--port", "6770"}, [3]int{6770, 21128, 4342}},
		{"gateway preserves legacy orca", []string{"--omniroute-port", "20129"}, [3]int{6768, 20129, 4342}},
		{"graph preserves legacy orca", []string{"--graph-port", "4243"}, [3]int{6768, 21128, 4243}},
		{"swap ports atomically", []string{"--port", "21128", "--omniroute-port", "6768"}, [3]int{21128, 6768, 4342}},
		{"all ports", []string{"--port", "6770", "--omniroute-port", "20129", "--graph-port", "4243"}, [3]int{6770, 20129, 4243}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, want := workspaceConfigFixture(t)
			want.Port, want.OmniRoutePort, want.GraphPort = tc.want[0], tc.want[1], tc.want[2]
			var out bytes.Buffer
			if err := a.WorkspaceConfig(tc.args, &out); err != nil {
				t.Fatal(err)
			}
			got, err := a.shared().Load()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%+v want=%+v err=%v", got, want, err)
			}
			for _, text := range []string{"adc workspace start", "активные сеансы"} {
				if !strings.Contains(out.String(), text) {
					t.Fatalf("missing restart guidance: %s", out.String())
				}
			}
		})
	}
}

func TestWorkspaceConfigReplacesOrClearsUSBWithoutDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct {
		name string
		args []string
		ids  []string
		port int
	}{
		{"replace", []string{"--usb", "1234:5678"}, []string{"1234:5678"}, 6768},
		{"replace multiple normalized", []string{"--usb", "ABCD:Ef01", "--usb=2104:0313"}, []string{"abcd:ef01", "2104:0313"}, 6768},
		{"clear", []string{"--usb-clear"}, nil, 6768},
		{"false preserves selection", []string{"--usb-clear=false"}, []string{"2104:0313"}, 6768},
		{"USB and port together", []string{"--usb", "ABCD:Ef01", "--port", "6770"}, []string{"abcd:ef01"}, 6770},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, want := workspaceConfigFixture(t)
			want.USBDevices, want.Port = tc.ids, tc.port
			var out bytes.Buffer
			if err := a.WorkspaceConfig(tc.args, &out); err != nil {
				t.Fatal(err)
			}
			got, err := a.shared().Load()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%+v want=%+v err=%v", got, want, err)
			}
			for _, text := range []string{"adc workspace start", "пересоздаст контейнер", "активные сеансы"} {
				if !strings.Contains(out.String(), text) {
					t.Fatalf("missing apply guidance %q: %s", text, out.String())
				}
			}
			if len(tc.ids) == 0 && !strings.Contains(out.String(), "USB: устройства не выбраны") {
				t.Fatal("missing empty selection", out.String())
			}
			for _, id := range tc.ids {
				if !strings.Contains(out.String(), id) {
					t.Fatalf("missing selected device %s: %s", id, out.String())
				}
			}
		})
	}
}

func TestWorkspaceConfigRejectsInvalidPortsWithoutWriting(t *testing.T) {
	for _, args := range [][]string{
		{"--port", "0"}, {"--port", "-1"}, {"--port", "65536"}, {"--port", "wat"}, {"--port"},
		{"--omniroute-port", "0"}, {"--omniroute-port", "65536"}, {"--graph-port", "0"}, {"--graph-port", "-1"},
		{"--port", "21128"}, {"--port", "4342"}, {"--omniroute-port", "6768"}, {"--graph-port", "21128"},
		{"--unknown", "1"}, {"name"}, {"--port", "6770", "extra"},
		{"--usb", ""}, {"--usb"}, {"--usb", "2104:313"}, {"--usb", "2104:0313,1234:5678"},
		{"--usb", "2104:0313", "--usb", "2104:0313"}, {"--usb", "ABCD:ef01", "--usb", "abcd:EF01"},
		{"--usb", "2104:0313", "--usb-clear"}, {"--usb-clear", "--usb", "2104:0313"},
		{"--usb", "2104:0313", "--usb-clear=false"}, {"--usb-clear=wrong"},
		{"--port", "6770", "--usb", "invalid"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			a, _ := workspaceConfigFixture(t)
			before, err := os.ReadFile(filepath.Join(a.State, "workspace.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := a.WorkspaceConfig(args, io.Discard); err == nil {
				t.Fatal("accepted invalid config")
			}
			after, err := os.ReadFile(filepath.Join(a.State, "workspace.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid config changed saved configuration", err)
			}
		})
	}
}

func TestWorkspaceConfigRejectsPendingRecoveryWithoutWriting(t *testing.T) {
	for _, kind := range []string{"migration", "replacement", "invalid replacement"} {
		t.Run(kind, func(t *testing.T) {
			a, c := workspaceConfigFixture(t)
			path, data := filepath.Join(a.State, "migration-pending.json"), []byte("{}")
			if kind != "migration" {
				path = a.replacementPath()
				if kind == "replacement" {
					data, _ = json.Marshal(workspaceReplacement{Version: 1, Container: "adc-existing", Token: "0123456789abcdef",
						Backup: "adc-existing-previous-0123456789abcdef", PreviousID: "old", Key: "new"})
				}
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"--port", "6770"}, {"--usb", "1234:5678"}, {"--usb-clear"}} {
				if err := a.WorkspaceConfig(args, io.Discard); err == nil {
					t.Fatalf("changed settings during pending recovery: %v", args)
				}
			}
			got, err := a.shared().Load()
			if err != nil || !reflect.DeepEqual(got, c) {
				t.Fatal("pending recovery lost saved configuration", err)
			}
			if err := a.WorkspaceConfig(nil, io.Discard); err != nil {
				t.Fatal("pending recovery prevented reading saved config", err)
			}
		})
	}
}

func TestWorkspaceConfigLocksAndReloadsConcurrentChanges(t *testing.T) {
	for _, lock := range []string{"lifecycle.lockfile", "write.lockfile"} {
		t.Run(lock, func(t *testing.T) {
			a, want := workspaceConfigFixture(t)
			done := make(chan error, 1)
			if err := a.shared().LockedFile(lock, func() error {
				go func() { done <- a.WorkspaceConfig([]string{"--port", "6770"}, io.Discard) }()
				select {
				case err := <-done:
					t.Fatalf("config bypassed %s: %v", lock, err)
				case <-time.After(150 * time.Millisecond):
				}
				want.Knowledge = append(want.Knowledge, shared.Knowledge{Path: "/new/knowledge", Origin: "concurrent registration"})
				return a.shared().Save(want)
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("config did not finish after lock release")
			}
			want.Port = 6770
			got, err := a.shared().Load()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("config overwrote concurrent registration", got, err)
			}
		})
	}
}

func TestWorkspaceConfigRequiresHost(t *testing.T) {
	a, _ := workspaceConfigFixture(t)
	a.Runtime = true
	for _, args := range [][]string{nil, {"--port", "6770"}, {"--usb", "1234:5678"}, {"--usb-clear"}} {
		if err := a.WorkspaceConfig(args, io.Discard); err == nil || !strings.Contains(err.Error(), "на хосте") {
			t.Fatal("runtime accepted host config command", err)
		}
	}
}

func TestWorkspaceStartReloadsSavedConfigBeforeDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a, stale := workspaceConfigFixture(t)
	current := stale
	current.Port = current.GraphPort
	if err := a.shared().Save(current); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(stale); err == nil || !strings.Contains(err.Error(), "порты Orca") {
		t.Fatal("start did not revalidate the saved config before calling Docker", err)
	}
}
