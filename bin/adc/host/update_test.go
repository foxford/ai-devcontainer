package host

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUpdateWorkspacePreservesLifecycleAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, state, fail, want string
	}{
		{"running", "running", "", "prepare,start"},
		{"restarting", "restarting", "", "prepare,start"},
		{"stopped", "exited", "", "prepare"},
		{"created", "created", "", "prepare"},
		{"absent", "", "", "prepare"},
		{"build or install failure", "running", "prepare", "prepare"},
		{"resume failure", "running", "start", "prepare,start"},
		{"paused", "paused", "paused", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			failure := errors.New("fixture failure")
			step := func(name string) func() error {
				return func() error {
					calls = append(calls, name)
					if tc.fail == name {
						return failure
					}
					return nil
				}
			}
			err := updateWorkspace(tc.state, step("prepare"), step("start"))
			if got := strings.Join(calls, ","); got != tc.want {
				t.Fatalf("lifecycle: %s; want %s", got, tc.want)
			}
			if (err != nil) != (tc.fail != "") {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.fail != "" && tc.fail != "paused" && !errors.Is(err, failure) {
				t.Fatalf("lost cause: %v", err)
			}
			if tc.fail == "start" && !strings.Contains(err.Error(), "adc workspace start") {
				t.Fatalf("missing recovery command: %v", err)
			}
		})
	}
}

func TestUpdateBuildsNewCommandBeforePublishing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX compiler fixture; executable replacement is tested natively below")
	}
	for _, compilerFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "compiler failure"}[compilerFails], func(t *testing.T) {
			platform, _, state := oldInstallFixture(t)
			bin := filepath.Join(state, "bin/adc")
			if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bin, []byte("previous CLI"), 0700); err != nil {
				t.Fatal(err)
			}
			compiler := filepath.Join(state, "toolchains/go1.26.1/go/bin/go")
			if err := os.MkdirAll(filepath.Dir(compiler), 0700); err != nil {
				t.Fatal(err)
			}
			trace := filepath.Join(state, "apply.log")
			text := "#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -o ]; then shift; out=$1; break; fi; shift; done\ncat > \"$out\" <<'ADC_HELPER'\n#!/bin/sh\nprintf '%s\\n' \"$1\" \"$AI_DEVCONTAINER_HOME\" \"$ADC_HOME\" > \"$ADC_UPDATE_FIXTURE_TRACE\"\nADC_HELPER\nchmod +x \"$out\"\n"
			if compilerFails {
				text = "#!/bin/sh\nexit 23\n"
			}
			if err := os.WriteFile(compiler, []byte(text), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ADC_UPDATE_FIXTURE_TRACE", trace)
			err := (&App{Platform: platform, State: state}).Update()
			if compilerFails {
				if err == nil {
					t.Fatal("compiler failure was ignored")
				}
				if _, err := os.Stat(trace); !os.IsNotExist(err) {
					t.Fatal("applied an uncompiled command")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(trace)
				if err != nil || string(data) != "bootstrap-apply-update\n"+platform+"\n"+state+"\n" {
					t.Fatal("new command was not invoked with the correct installation context")
				}
			}
			if data, err := os.ReadFile(bin); err != nil || string(data) != "previous CLI" {
				t.Fatal("builder overwrote the working CLI before apply")
			}
			leftovers, _ := filepath.Glob(filepath.Join(state, "bin/.adc-update-*"))
			if len(leftovers) != 0 {
				t.Fatal("temporary command was not cleaned up")
			}
		})
	}
}

func TestInstallCommandReplacesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adc.exe")
	if err := os.WriteFile(path, []byte("old executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := installCommandFile(path, []byte("new executable")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new executable" {
		t.Fatal("did not replace command")
	}
}

func TestInstallCommandWhileOldWindowsExecutableRuns(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable sharing semantics")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "adc.exe")
	if err = os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path, "-test.run=^TestInstallCommandWorker$")
	cmd.Env = append(os.Environ(), "ADC_UPDATE_WORKER=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		cmd.Process.Kill()
		_ = cmd.Wait()
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if strings.TrimSpace(line) != "ready" {
			t.Fatalf("old executable did not start: %q", line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("old executable startup timed out")
	}
	if err = installCommandFile(path, []byte("new command")); err != nil {
		t.Fatalf("replace running Windows CLI: %v", err)
	}
	input.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new command" {
		t.Fatal("new command was not installed")
	}
}

func TestInstallCommandWorker(t *testing.T) {
	if os.Getenv("ADC_UPDATE_WORKER") != "1" {
		return
	}
	io.WriteString(os.Stdout, "ready\n")
	io.Copy(io.Discard, os.Stdin)
}

func TestUpdateRejectsLocalChangesAndUnfinishedReplacementBeforePull(t *testing.T) {
	for _, scenario := range []string{"tracked changes", "untracked files", "pending replacement"} {
		t.Run(scenario, func(t *testing.T) {
			platform, _, state := oldInstallFixture(t)
			before := legacyGit(t, platform, "rev-parse", "HEAD")
			path := filepath.Join(platform, "go.mod")
			want := "локальные изменения"
			if scenario == "untracked files" {
				path = filepath.Join(platform, "local-notes.txt")
			}
			if scenario == "pending replacement" {
				path = filepath.Join(state, "workspace-replacement.json")
				want = "adc workspace start"
				if err := os.MkdirAll(state, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, []byte("keep this local file"), 0600); err != nil {
				t.Fatal(err)
			}
			err := (&App{Platform: platform, State: state}).Update()
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal(err)
			}
			if legacyGit(t, platform, "rev-parse", "HEAD") != before {
				t.Fatal("changed HEAD before preflight")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "keep this local file" {
				t.Fatal("local data changed", err)
			}
		})
	}
}
