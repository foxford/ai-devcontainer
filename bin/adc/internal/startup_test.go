package shared

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStartupStatusPublishesPreparationFailureAndReadiness(t *testing.T) {
	file := filepath.Join(t.TempDir(), "startup.json")
	stopStartup := ReportStartup(file)
	defer stopStartup()
	for _, expected := range []StartupStatus{{State: "preparing", Stage: "Установка claude"}, {State: "failed", Stage: "fixture DNS error"}, {State: "ready", Stage: "Среда готова"}} {
		switch expected.State {
		case "preparing":
			StartupProgress(expected.Stage)
		case "failed":
			StartupFinished(errors.New(expected.Stage))
		case "ready":
			StartupFinished(nil)
		}
		data, err := os.ReadFile(file)
		var actual StartupStatus
		if err != nil || json.Unmarshal(data, &actual) != nil || actual.State != expected.State || actual.Stage != expected.Stage || actual.Updated.IsZero() {
			t.Fatalf("status %s error %v", data, err)
		}
	}
}

func TestStartupReportingDoesNotEscapeToChildProcess(t *testing.T) {
	if os.Getenv("ADC_STARTUP_TEST_CHILD") == "1" {
		if StartupProgress("Graphify: ordinary agent sync") {
			t.Fatal("child process inherited startup reporting")
		}
		StartupFinished(errors.New("ordinary child command failed"))
		return
	}
	file := filepath.Join(t.TempDir(), "startup.json")
	// Sessions from older Orca servers may still carry the leaked variable.
	t.Setenv("ADC_STARTUP_STATUS", file)
	stopStartup := ReportStartup(file)
	defer stopStartup()
	StartupFinished(nil)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestStartupReportingDoesNotEscapeToChildProcess$")
	cmd.Env = append(os.Environ(), "ADC_STARTUP_TEST_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process: %v\n%s", err, output)
	}
	assertUnchanged := func() {
		t.Helper()
		after, err := os.ReadFile(file)
		if err != nil || string(after) != string(before) {
			t.Fatalf("readiness changed after ordinary sync: %s, %v", after, err)
		}
	}
	assertUnchanged()
	stopStartup()
	if StartupProgress("background work after initialization") {
		t.Fatal("startup reporting remained active after stop")
	}
	StartupFinished(errors.New("background work failed"))
	assertUnchanged()
}
