package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdatesCommandHelpAndFailureJSON(t *testing.T) {
	for _, side := range []string{"0", "1"} {
		t.Run(side, func(t *testing.T) {
			t.Setenv("ADC_RUNTIME", side)
			t.Setenv("AI_DEVCONTAINER_HOME", t.TempDir())
			t.Setenv("ADC_HOME", t.TempDir())
			var out bytes.Buffer
			root := commandTree()
			root.SetOut(&out)
			root.SetArgs([]string{"updates", "--help"})
			if err := root.Execute(); err != nil || !strings.Contains(out.String(), "--refresh") || !strings.Contains(out.String(), "--json") {
				t.Fatal(out.String(), err)
			}
			out.Reset()
			root = commandTree()
			root.SetOut(&out)
			root.SetArgs([]string{"updates", "--json"})
			if err := root.Execute(); err == nil {
				t.Fatal("accepted a missing platform")
			}
			var result struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Error == "" || strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "applied") {
				t.Fatalf("failure did not produce plain JSON: %q %v", out.String(), err)
			}
		})
	}
	t.Setenv("ADC_RUNTIME", "0")
	var out bytes.Buffer
	root := commandTree()
	root.SetOut(&out)
	root.SetArgs([]string{"update", "--help"})
	if err := root.Execute(); err != nil || !strings.Contains(out.String(), "--check") {
		t.Fatal(out.String(), err)
	}
	root = commandTree()
	root.SetArgs([]string{"update", "--json"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--check") {
		t.Fatal("--json must not apply an update", err)
	}
}

func TestDeliveryUpdateFlagsAndRuntimeBoundary(t *testing.T) {
	for _, side := range []string{"0", "1"} {
		t.Run(side, func(t *testing.T) {
			t.Setenv("ADC_RUNTIME", side)
			var out bytes.Buffer
			root := commandTree()
			root.SetOut(&out)
			root.SetArgs([]string{"update", "--help"})
			if err := root.Execute(); err != nil || !strings.Contains(out.String(), "--delivery") {
				t.Fatal("delivery flag missing", out.String(), err)
			}
			for _, flag := range []string{"--check", "--json"} {
				root := commandTree()
				root.SetArgs([]string{"update", "--delivery", flag})
				if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "нельзя совмещать") {
					t.Fatalf("ambiguous update flags accepted: %s %v", flag, err)
				}
			}
			if side == "1" {
				root := commandTree()
				root.SetArgs([]string{"update"})
				if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "на хосте") {
					t.Fatal("full update was allowed inside runtime", err)
				}
			}
		})
	}
}
