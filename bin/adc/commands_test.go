package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandHelpAndCompletion(t *testing.T) {
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
