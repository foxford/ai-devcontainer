package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestWorkspaceWaitReportsProgressAndReturnsWhenReady(t *testing.T) {
	var out bytes.Buffer
	states := []workspaceStartup{
		{Status: "running", Progress: shared.StartupStatus{State: "preparing", Stage: "Запуск Orca"}},
		{Status: "running", Progress: shared.StartupStatus{State: "preparing", Stage: "Установка claude"}},
		{Status: "running", Progress: shared.StartupStatus{State: "preparing", Stage: "Установка claude"}},
		{Status: "running", Progress: shared.StartupStatus{State: "ready"}},
	}
	calls := 0
	err := waitWorkspace(context.Background(), func(context.Context) (workspaceStartup, error) {
		state := states[calls]
		calls++
		return state, nil
	}, &out, time.Millisecond, 0)
	if err != nil || calls != 4 || !strings.Contains(out.String(), "Запуск Orca") || strings.Count(out.String(), "Установка claude") != 2 {
		t.Fatalf("calls=%d output=%s error=%v", calls, out.String(), err)
	}
}

func TestWorkspaceIgnoresStatusFromPreviousContainerBoot(t *testing.T) {
	started := time.Now()
	for _, tc := range []struct {
		updated time.Time
		current bool
	}{
		{started.Add(-time.Second), false},
		{started.Add(time.Second), true},
		{time.Time{}, false},
	} {
		data, err := json.Marshal(shared.StartupStatus{State: "ready", Stage: "ready", Updated: tc.updated})
		if err != nil {
			t.Fatal(err)
		}
		if _, current := startupForBoot(data, started); current != tc.current {
			t.Fatal("accepted stale readiness", string(data))
		}
	}
	if _, current := startupForBoot([]byte("broken JSON"), started); current {
		t.Fatal("accepted corrupt status")
	}
}

func TestWorkspaceWaitStopsOnFailureOrCancellation(t *testing.T) {
	for _, tc := range []struct {
		state workspaceStartup
		want  string
	}{
		{workspaceStartup{Status: "running", Progress: shared.StartupStatus{State: "failed", Stage: "npm ETIMEDOUT"}}, "ETIMEDOUT"},
		{workspaceStartup{Status: "restarting", Progress: shared.StartupStatus{Stage: "Установка claude"}}, "restarting"},
	} {
		var out bytes.Buffer
		err := waitWorkspace(context.Background(), func(context.Context) (workspaceStartup, error) { return tc.state, nil }, &out, time.Millisecond, time.Second)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	err := waitWorkspace(ctx, func(context.Context) (workspaceStartup, error) {
		cancel()
		return workspaceStartup{Status: "running", Progress: shared.StartupStatus{Stage: "Установка"}}, nil
	}, &out, time.Hour, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("did not stop waiting", err)
	}
	for _, state := range []workspaceStartup{
		{Status: "running", Progress: shared.StartupStatus{State: "preparing", Stage: "Установка"}},
		{Status: "running", Progress: shared.StartupStatus{State: "ready"}},
		{Status: "exited"},
	} {
		text := startupSummary(state)
		if state.Progress.State == "preparing" && !strings.Contains(text, "подготовка") || state.Progress.State == "ready" && !strings.Contains(text, "готов") || state.Status == "exited" && text != "exited" {
			t.Fatal(text)
		}
	}
}
