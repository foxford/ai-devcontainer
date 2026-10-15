package host

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestWorkspaceLogsKeepsPairingBeforeLastHundredLines(t *testing.T) {
	const ready = `{"type":"orca_server_ready","pairing":{"available":true,"url":"orca://pair?code=test-only"}}`
	lines := append([]string{ready}, strings.Split(strings.Repeat("preparing project\n", 150), "\n")...)
	for _, tc := range []struct {
		name        string
		args        []string
		wantPairing bool
	}{
		{"default", nil, true},
		{"explicit all", []string{"--tail", "all"}, true},
		{"explicit limit", []string{"--tail", "100"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runWorkspaceLogs("adc-test", tc.args, func(dir, name string, args ...string) error {
				if dir != "" || name != "docker" || len(args) != 4 || args[0] != "logs" || args[1] != "--tail" || args[3] != "adc-test" {
					t.Fatalf("unexpected Docker invocation: %q %q %q", dir, name, args)
				}
				selected := lines
				if args[2] != "all" {
					limit, err := strconv.Atoi(args[2])
					if err != nil {
						return err
					}
					selected = lines[len(lines)-limit:]
				}
				_, err := out.WriteString(strings.Join(selected, "\n"))
				return err
			})
			if err != nil || strings.Contains(out.String(), ready) != tc.wantPairing {
				t.Fatalf("pairing present=%v want=%v error=%v", strings.Contains(out.String(), ready), tc.wantPairing, err)
			}
		})
	}
}

func TestWorkspaceLogsFollowAndDockerFailure(t *testing.T) {
	wantErr := errors.New("Docker unavailable")
	err := runWorkspaceLogs("adc-test", []string{"--tail", "0", "--follow"}, func(dir, name string, args ...string) error {
		if dir != "" || name != "docker" || !reflect.DeepEqual(args, []string{"logs", "--tail", "0", "--follow", "adc-test"}) {
			t.Fatalf("unexpected Docker invocation: %q %q %q", dir, name, args)
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Docker error lost: %v", err)
	}
}

func TestWorkspaceLogsRejectsInvalidOptionsBeforeDocker(t *testing.T) {
	for _, args := range [][]string{
		{"--tail", "-1"}, {"--tail", "wat"}, {"--tail", ""}, {"--tail", "1.5"},
		{"--tail", "18446744073709551616"}, {"--tail"}, {"--unknown"}, {"unexpected"},
	} {
		err := runWorkspaceLogs("adc-test", args, func(string, string, ...string) error {
			t.Fatal("Docker called for invalid arguments", args)
			return nil
		})
		if err == nil {
			t.Fatal("accepted invalid arguments", args)
		}
	}
}
