package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func runCompletion(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := commandTree()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(append([]string{"completion"}, args...))
	err := root.Execute()
	return output.String(), err
}

func TestCompletionSavesScriptsWithoutChangingProfiles(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			home := testutil.Home(t)
			zdir, config := filepath.Join(home, "zsh config"), filepath.Join(home, "xdg config")
			t.Setenv("ZDOTDIR", zdir)
			t.Setenv("XDG_CONFIG_HOME", config)
			profiles := []string{filepath.Join(home, ".bashrc"), filepath.Join(zdir, ".zshrc")}
			for _, path := range profiles {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("# keep my profile\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			filename := "adc." + shell
			if shell == "powershell" {
				filename = "adc.ps1"
			}
			path := filepath.Join(home, ".adc", "completions", filename)
			if shell == "fish" {
				path = filepath.Join(config, "fish", "completions", filename)
			}
			output, err := runCompletion(t, shell)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || len(data) < 500 {
				t.Fatalf("script was not saved: %v", err)
			}
			if !strings.Contains(output, path) || len(output) > 2000 || strings.Contains(output, "__adc_debug") || strings.Contains(output, "Register-ArgumentCompleter") {
				t.Fatalf("printed script instead of instructions: %s", output)
			}
			before, _ := os.Stat(path)
			if _, err := runCompletion(t, shell); err != nil {
				t.Fatal(err)
			}
			after, _ := os.Stat(path)
			if !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("unchanged script rewritten")
			}
			for _, profile := range profiles {
				if data, err := os.ReadFile(profile); err != nil || string(data) != "# keep my profile\n" {
					t.Fatal("profile modified", err)
				}
			}
		})
	}
}

func TestCompletionStdoutDoesNotWriteFiles(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			home := testutil.Home(t)
			output, err := runCompletion(t, shell, "--stdout", "--no-descriptions")
			if err != nil || len(output) < 500 || strings.Contains(output, "Скрипт сохранён") {
				t.Fatalf("not a raw script: %v", err)
			}
			if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
				t.Fatalf("stdout modified home: %v %v", entries, err)
			}
		})
	}
}

func TestRuntimeCompletionDoesNotReplaceHostScript(t *testing.T) {
	home := testutil.Home(t)
	t.Setenv("ADC_RUNTIME", "1")
	hostFile := filepath.Join(home, ".adc", "completions", "adc.zsh")
	if err := os.MkdirAll(filepath.Dir(hostFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostFile, []byte("host commands"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runCompletion(t, "zsh")
	if err != nil || !strings.Contains(output, filepath.Join(home, ".local", "share", "adc", "completions", "adc.zsh")) {
		t.Fatal(output, err)
	}
	if data, err := os.ReadFile(hostFile); err != nil || string(data) != "host commands" {
		t.Fatal("runtime overwrote host script", err)
	}
}

func TestCompletionSuggestedCommandsLoadBashAndZsh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shells")
	}
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell unavailable")
			}
			home := testutil.Home(t)
			state := filepath.Join(home, "state with 'quotes'")
			t.Setenv("ADC_HOME", state)
			output, err := runCompletion(t, shell)
			if err != nil {
				t.Fatal(err)
			}
			var instructions []string
			for _, line := range strings.Split(output, "\n") {
				if strings.HasPrefix(line, "  ") {
					instructions = append(instructions, strings.TrimPrefix(line, "  "))
				}
			}
			if len(instructions) != 2 {
				t.Fatal("missing session/profile command", output)
			}
			check := "type __adc_debug >/dev/null"
			if shell == "zsh" {
				check = "(( $+functions[_adc] && $+functions[compdef] ))"
			}
			cmd := exec.Command(binary, "-c", instructions[0]+"\n"+check+"\n"+instructions[1])
			if data, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("instructions do not work: %v %s", err, data)
			}
			profile := filepath.Join(home, map[string]string{"bash": ".bashrc", "zsh": ".zshrc"}[shell])
			cmd = exec.Command(binary, "-c", "source '"+profile+"'\n"+check)
			if data, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("profile does not load completion: %v %s", err, data)
			}
		})
	}
}
