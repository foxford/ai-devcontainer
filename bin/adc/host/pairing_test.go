package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

const pairingEvent = `{"type":"orca_server_ready","pairing":{"available":true,"url":"orca://pair?code=test-only"}}`

func TestReadOrcaPairingAcrossNoisyLogs(t *testing.T) {
	for _, logs := range []string{
		pairingEvent,
		pairingEvent + "\n" + strings.Repeat("preparing project\n", 200),
		"not JSON\n{broken\n" + pairingEvent + "\r\n",
		strings.Repeat("x", 2*1024*1024) + "\n" + pairingEvent,
		pairingEvent + "\n" + strings.Repeat("x", 2*1024*1024),
		`{"type":"other","pairing":{"available":true,"url":"orca://pair?code=wrong"}}` + "\n" + pairingEvent,
	} {
		got, err := readOrcaPairing(strings.NewReader(logs))
		if err != nil || got != "orca://pair?code=test-only" {
			t.Fatalf("pairing=%q error=%v", got, err)
		}
	}
}

func TestReadOrcaPairingUsesLatestReadyEvent(t *testing.T) {
	for _, tc := range []struct {
		name, pairing, want string
	}{
		{"new link", `{"available":true,"url":"orca://pair?code=new-test%2Bvalue%3D&v=1"}`, "orca://pair?code=new-test%2Bvalue%3D&v=1"},
		{"unavailable", `{"available":false,"url":"orca://pair?code=old"}`, ""},
		{"no URL", `{"available":true}`, ""},
		{"missing pairing", `null`, ""},
		{"malformed pairing", `"unexpected"`, ""},
		{"malformed field", `{"available":true,"url":123}`, ""},
		{"wrong scheme", `{"available":true,"url":"https://pair?code=test"}`, ""},
		{"missing code", `{"available":true,"url":"orca://pair?code="}`, ""},
		{"terminal escape", `{"available":true,"url":"orca://pair?code=test\u001b[2J"}`, ""},
		{"multiple lines", `{"available":true,"url":"orca://pair?code=test\nextra"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := pairingEvent + "\n" + `{"type":"orca_server_ready","pairing":` + tc.pairing + `}`
			got, err := readOrcaPairing(strings.NewReader(logs))
			if err != nil || got != tc.want {
				t.Fatalf("pairing=%q want=%q error=%v", got, tc.want, err)
			}
		})
	}
	got, err := readOrcaPairing(strings.NewReader("no ready event\n"))
	if err != nil || got != "" {
		t.Fatalf("pairing=%q error=%v", got, err)
	}
}

type pairingReadError struct{ err error }

func (r pairingReadError) Read([]byte) (int, error) { return 0, r.err }

func TestReadOrcaPairingDiscardsPartialResultOnReadFailure(t *testing.T) {
	wantErr := errors.New("log read failed")
	got, err := readOrcaPairing(io.MultiReader(strings.NewReader(pairingEvent+"\n"), pairingReadError{wantErr}))
	if got != "" || !errors.Is(err, wantErr) {
		t.Fatalf("pairing=%q error=%v", got, err)
	}
}

func TestPairingCommandHelper(t *testing.T) {
	mode := os.Getenv("ADC_TEST_PAIRING_COMMAND")
	if mode == "" {
		return
	}
	if mode == "stderr" {
		fmt.Fprintln(os.Stderr, pairingEvent)
	} else {
		fmt.Fprintln(os.Stdout, pairingEvent)
	}
	fmt.Fprint(os.Stderr, strings.Repeat("server log\n", 200))
	if mode == "fail" {
		os.Exit(23)
	}
	if mode == "timeout" {
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func TestPairingCommandReadsBothStreamsAndRejectsIncompleteCommands(t *testing.T) {
	for _, mode := range []string{"stdout", "stderr", "fail", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPairingCommandHelper$")
			cmd.Env = append(os.Environ(), "ADC_TEST_PAIRING_COMMAND="+mode)
			got, err := pairingFromCommand(cmd, cancel)
			if mode == "fail" || mode == "timeout" {
				if err == nil || got != "" {
					t.Fatalf("incomplete command supplied pairing=%q error=%v", got, err)
				}
			} else if err != nil || got != "orca://pair?code=test-only" {
				t.Fatalf("pairing=%q error=%v", got, err)
			}
		})
	}
}

func TestDockerPairingRequiresCurrentBoot(t *testing.T) {
	if got, err := dockerPairing(context.Background(), "adc-test", time.Time{}); got != "" || err == nil {
		t.Fatalf("read logs without a boot boundary: pairing=%q error=%v", got, err)
	}
}

func TestWorkspaceReadyShowsCurrentPairingWithoutFailingReadiness(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX Docker fixture; parser and subprocess tests also run on Windows")
	}
	for _, mode := range []string{"ready", "missing", "logs-failed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			// The fixture returns an old link if --since is absent or inaccurate.
			script := `#!/bin/sh
case "$1" in
inspect) printf '%s\n' '{"Status":"running","StartedAt":"2026-10-15T10:00:00.123456789Z"}' ;;
exec) printf '%s\n' '{"state":"ready","updated":"2026-10-15T10:01:00Z"}' ;;
logs)
  if [ "$2" != --since ] || [ "$3" != 2026-10-15T10:00:00.123456789Z ] || [ "$4" != adc-pairing-test ]; then
    printf '%s\n' '{"type":"orca_server_ready","pairing":{"available":true,"url":"orca://pair?code=stale"}}'
  elif [ "$ADC_TEST_PAIRING_MODE" = ready ]; then
    printf '%s\n' '` + pairingEvent + `'
  elif [ "$ADC_TEST_PAIRING_MODE" = logs-failed ]; then
    exit 23
  fi ;;
*) exit 24 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("ADC_TEST_PAIRING_MODE", mode)
			out, err := os.CreateTemp(t.TempDir(), "startup-output")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			stdout := os.Stdout
			os.Stdout = out
			defer func() { os.Stdout = stdout }()
			ready, err := (&App{}).waitForWorkspaceReady(shared.Config{Name: "pairing-test", Port: 6770})
			os.Stdout = stdout
			if err != nil || !ready {
				t.Fatalf("lost readiness: ready=%v error=%v", ready, err)
			}
			data, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("Готово.")) || !bytes.Contains(data, []byte("127.0.0.1:6770")) || bytes.Contains(data, []byte("stale")) || bytes.Contains(data, []byte("pairing --help")) {
				t.Fatalf("incorrect ready output: %s", data)
			}
			if mode == "ready" {
				if !bytes.Contains(data, []byte("Access link: orca://pair?code=test-only\n")) {
					t.Fatalf("missing current pairing: %s", data)
				}
			} else if !bytes.Contains(data, []byte("adc workspace logs")) || bytes.Contains(data, []byte("orca://pair")) {
				t.Fatalf("missing recovery hint or printed stale URL: %s", data)
			}
		})
	}
}
