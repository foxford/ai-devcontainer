package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandHelpAndCompletion(t *testing.T) {
	t.Setenv("ADC_RUNTIME", "0")
	for _, args := range [][]string{{"--help"}, {"project", "add", "--help"}, {"skill", "fork", "--help"}, {"mcp", "add", "--help"}, {"mcp", "list", "--help"}, {"plans", "--help"}, {"tools", "--help"}, {"tools", "graphify", "--help"}, {"tools", "install", "--help"}, {"tools", "claude-snapshot", "--help"}, {"tools", "pnpm-patch-dep", "--help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
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
			if args[0] == "mcp" && !strings.Contains(buf.String(), "--json") {
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
			cases := map[string]bool{"banner": side == "1", "install": side == "0", "update": side == "0", "ensure-image": side == "0", "project add": side == "0", "project list": true, "workspace start": side == "0", "workspace map": true, "migrate legacy import-host": side == "0", "doctor project": true, "sync": true, "agent": true}
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
