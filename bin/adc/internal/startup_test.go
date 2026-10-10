package shared

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStartupStatusPublishesPreparationFailureAndReadiness(t *testing.T) {
	file := filepath.Join(t.TempDir(), "startup.json")
	t.Setenv("ADC_STARTUP_STATUS", file)
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
