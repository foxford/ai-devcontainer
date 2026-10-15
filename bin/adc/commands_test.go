package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestCommandHelpAndCompletion(t *testing.T) {
	t.Setenv("ADC_RUNTIME", "0")
	for _, args := range [][]string{{"--help"}, {"project", "add", "--help"}, {"skill", "fork", "--help"}, {"mcp", "add", "--help"}, {"mcp", "list", "--help"}, {"mcp", "doctor", "--help"}, {"plans", "--help"}, {"tools", "--help"}, {"tools", "graphify", "--help"}, {"tools", "install", "--help"}, {"tools", "claude-snapshot", "--help"}, {"tools", "pnpm-patch-dep", "--help"}, {"completion", "bash", "--stdout"}, {"completion", "zsh", "--stdout"}, {"completion", "fish", "--stdout"}, {"completion", "powershell", "--stdout"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var buf bytes.Buffer
			cmd := commandTree()
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(args)
			if e := cmd.Execute(); e != nil {
				t.Fatal(e)
			}
			if buf.Len() == 0 {
				t.Fatal("no help/completion output")
			}
			if args[0] == "project" && !strings.Contains(buf.String(), "--readonly") {
				t.Fatal("declared flag missing from generated help")
			}
			if args[0] == "mcp" && args[1] != "doctor" && !strings.Contains(buf.String(), "--json") {
				t.Fatal("MCP flags missing from generated help")
			}
		})
	}
	for _, args := range [][]string{{"project", "add"}, {"project", "add", "a", "b"}, {"workspace", "init", "--port", "wat"}, {"workspace", "unknown"}, {"project", "add", "a", "--unknown"}, {"skill", "fork"}, {"skill", "list", "extra"}, {"mcp", "rm"}, {"mcp", "disable", "a", "b"}, {"mcp", "add", "x", "--local", "--project", "--url", "https://example.invalid"}, {"mcp", "add", "x", "--json", "{}", "--url", "https://example.invalid"}, {"plans", "extra"}} {
		cmd := commandTree()
		cmd.SetArgs(args)
		if e := cmd.Execute(); e == nil {
			t.Fatalf("accepted invalid invocation %v", args)
		}
	}
}

func TestCommandAvailabilityAndCompletionMatchExecutionSide(t *testing.T) {
	for _, side := range []string{"0", "1"} {
		t.Run(side, func(t *testing.T) {
			t.Setenv("ADC_RUNTIME", side)
			cases := map[string]bool{"banner": side == "1", "install": side == "0", "update": true, "ensure-image": side == "0", "project add": side == "0", "project list": true, "project capability list": true, "project capability add": true, "project capability rm": true, "workspace start": side == "0", "workspace logs": side == "0", "workspace config": side == "0", "workspace map": true, "migrate legacy import-host": side == "0", "doctor project": true, "sync": true, "agent": true}
			for name, want := range cases {
				cmd, rest, err := commandTree().Find(strings.Fields(name))
				available := err == nil && len(rest) == 0 && cmd.Name() == strings.Fields(name)[len(strings.Fields(name))-1]
				if available != want {
					t.Fatalf("side %s command %s available=%v wanted=%v", side, name, available, want)
				}
			}
			for _, candidate := range []string{"banner", "install"} {
				var buf bytes.Buffer
				root := commandTree()
				root.SetOut(&buf)
				root.SetErr(&buf)
				root.SetArgs([]string{"__complete", candidate})
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(buf.String(), candidate+"\t") != cases[candidate] {
					t.Fatalf("completion disagrees with availability: %s", buf.String())
				}
			}
		})
	}
}

func TestWorkspacePortDefaultsAndConfigCLI(t *testing.T) {
	t.Setenv("ADC_RUNTIME", "0")
	t.Setenv("ADC_HOME", t.TempDir())
	if err := Run([]string{"workspace", "init", "configured"}); err != nil {
		t.Fatal(err)
	}
	a, err := shared.New()
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Load()
	if err != nil || c.Port != 6770 || c.Name != "configured" {
		t.Fatalf("CLI init defaults: %+v err=%v", c, err)
	}
	for _, tc := range []struct {
		args []string
		want [3]int
	}{
		{[]string{"--port", "6781", "--omniroute-port", "21128", "--graph-port", "4342"}, [3]int{6781, 21128, 4342}},
		{[]string{"--port=6770"}, [3]int{6770, 21128, 4342}},
		{[]string{"--omniroute-port=20129"}, [3]int{6770, 20129, 4342}},
		{[]string{"--graph-port", "4243"}, [3]int{6770, 20129, 4243}},
		{nil, [3]int{6770, 20129, 4243}},
	} {
		root := commandTree()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs(append([]string{"workspace", "config"}, tc.args...))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		got, err := a.Load()
		want := c
		want.Port, want.OmniRoutePort, want.GraphPort = tc.want[0], tc.want[1], tc.want[2]
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("args=%v config=%+v want=%+v err=%v", tc.args, got, want, err)
		}
		if !strings.Contains(out.String(), "Сохранённые порты") {
			t.Fatal("config output missing", out.String())
		}
	}
	before, err := os.ReadFile(filepath.Join(a.State, "workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--port", "wat"}, {"--port", "0"}, {"--port", "65536"}, {"--port", "20129"}, {"--unknown"}, {"extra"}} {
		if err := Run(append([]string{"workspace", "config"}, args...)); err == nil {
			t.Fatalf("accepted invalid CLI config: %v", args)
		}
		after, err := os.ReadFile(filepath.Join(a.State, "workspace.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("invalid invocation changed saved config", err)
		}
	}
}

func TestWorkspaceConfigHelpAndCompletion(t *testing.T) {
	t.Setenv("ADC_RUNTIME", "0")
	for _, command := range []string{"init", "config"} {
		var help bytes.Buffer
		root := commandTree()
		root.SetOut(&help)
		root.SetArgs([]string{"workspace", command, "--help"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, flag := range []string{"--port", "--omniroute-port", "--graph-port"} {
			if !strings.Contains(help.String(), flag) {
				t.Fatalf("%s help missing %s", command, flag)
			}
		}
		if command == "init" && !strings.Contains(help.String(), "6770") {
			t.Fatal("init help missing host port default", help.String())
		}
		if command == "config" && (!strings.Contains(help.String(), "adc workspace start") || !strings.Contains(help.String(), "активные сеансы")) {
			t.Fatal("config help missing apply guidance", help.String())
		}
	}
	for _, side := range []string{"0", "1"} {
		t.Setenv("ADC_RUNTIME", side)
		var out bytes.Buffer
		root := commandTree()
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"__complete", "workspace", "conf"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "config\t") != (side == "0") {
			t.Fatal("config completion does not match execution side", side, out.String())
		}
	}
}

func TestWorkspaceLogsFlagsAndHelp(t *testing.T) {
	t.Setenv("ADC_RUNTIME", "0")
	for _, tc := range []struct {
		args   []string
		tail   string
		follow bool
	}{
		{nil, "all", false},
		{[]string{"--tail", "100"}, "100", false},
		{[]string{"--tail=all", "--follow"}, "all", true},
		{[]string{"-f", "--tail", "0"}, "0", true},
	} {
		cmd, _, err := commandTree().Find([]string{"workspace", "logs"})
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.ParseFlags(tc.args); err != nil {
			t.Fatal(err)
		}
		tail, _ := cmd.Flags().GetString("tail")
		follow, _ := cmd.Flags().GetBool("follow")
		if tail != tc.tail || follow != tc.follow {
			t.Fatalf("args %v: tail=%q follow=%v", tc.args, tail, follow)
		}
	}
	var help bytes.Buffer
	cmd := commandTree()
	cmd.SetOut(&help)
	cmd.SetArgs([]string{"workspace", "logs", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--tail", "--follow", "-f", "all"} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("logs help omits %q: %s", want, help.String())
		}
	}
}

func TestCapabilityHelpAndCompletion(t *testing.T) {
	for _, side := range []string{"0", "1"} {
		t.Setenv("ADC_RUNTIME", side)
		for _, action := range []string{"list", "add", "rm"} {
			var buf bytes.Buffer
			cmd := commandTree()
			cmd.SetOut(&buf)
			cmd.SetArgs([]string{"project", "capability", action, "--help"})
			if err := cmd.Execute(); err != nil || !strings.Contains(buf.String(), "adc project capability "+action) {
				t.Fatal(buf.String(), err)
			}
		}
		var buf bytes.Buffer
		cmd := commandTree()
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"__complete", "project", "capability", "add", "gra"})
		if err := cmd.Execute(); err != nil || !strings.Contains(buf.String(), "graphify\t") {
			t.Fatal(buf.String(), err)
		}
		cmd = commandTree()
		cmd.SetArgs([]string{"project", "capability", "add"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("accepted add without a capability")
		}
	}
}

func TestSyncRemainsCallableButHiddenFromPublicCommands(t *testing.T) {
	for _, side := range []string{"0", "1"} {
		t.Run(side, func(t *testing.T) {
			t.Setenv("ADC_RUNTIME", side)
			root := commandTree()
			sync, rest, err := root.Find([]string{"sync"})
			if err != nil || len(rest) != 0 || sync.Name() != "sync" || !sync.Hidden {
				t.Fatalf("internal sync is unavailable or visible: %v", err)
			}
			if flag := sync.Flags().Lookup("all"); flag == nil {
				t.Fatal("internal sync --all flag is missing")
			}
			for _, args := range [][]string{{"--help"}, {"__complete", ""}, {"__complete", "sy"}} {
				var buf bytes.Buffer
				root := commandTree()
				root.SetOut(&buf)
				root.SetErr(&buf)
				root.SetArgs(args)
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				for _, line := range strings.Split(buf.String(), "\n") {
					if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "sync" {
						t.Fatalf("internal sync exposed by %v: %s", args, buf.String())
					}
				}
			}
			mcpSync, rest, err := commandTree().Find([]string{"mcp", "sync"})
			if err != nil || len(rest) != 0 || mcpSync.Name() != "sync" || mcpSync.Hidden {
				t.Fatalf("public mcp sync was hidden: %v", err)
			}
		})
	}
}
