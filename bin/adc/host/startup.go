package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type workspaceStartup struct {
	Status   string
	Progress shared.StartupStatus
}

func dockerStartup(ctx context.Context, container string) (workspaceStartup, error) {
	output := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	}
	var state struct {
		Status    string
		StartedAt time.Time
	}
	data, err := output("inspect", "--format", "{{json .State}}", container)
	if err != nil {
		if ctx.Err() != nil {
			return workspaceStartup{}, ctx.Err()
		}
		return workspaceStartup{}, fmt.Errorf("состояние Docker: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return workspaceStartup{}, err
	}
	result := workspaceStartup{Status: state.Status, Progress: shared.StartupStatus{State: "preparing", Stage: "Запуск runtime"}}
	if data, err := output("exec", container, "cat", shared.StartupPath); err == nil {
		if progress, current := startupForBoot(data, state.StartedAt); current {
			result.Progress = progress
		}
	}
	return result, nil
}

func startupForBoot(data []byte, started time.Time) (shared.StartupStatus, bool) {
	var progress shared.StartupStatus
	if json.Unmarshal(data, &progress) != nil || progress.Updated.IsZero() || progress.Updated.Before(started) {
		return progress, false
	}
	return progress, progress.State == "preparing" || progress.State == "ready" || progress.State == "failed"
}

func startupSummary(state workspaceStartup) string {
	if state.Progress.State == "failed" {
		return state.Status + " (ошибка подготовки: " + state.Progress.Stage + ")"
	}
	if state.Status != "running" {
		return state.Status
	}
	if state.Progress.State == "ready" {
		return "running (готов)"
	}
	return "running (подготовка: " + state.Progress.Stage + ")"
}

func waitWorkspace(ctx context.Context, poll func(context.Context) (workspaceStartup, error), out io.Writer, interval, heartbeat time.Duration) error {
	started, printed := time.Now(), time.Time{}
	stage := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state, err := poll(ctx)
		if err != nil {
			return err
		}
		if state.Progress.State == "failed" {
			return fmt.Errorf("подготовка среды: %s", state.Progress.Stage)
		}
		if state.Status != "running" {
			return fmt.Errorf("runtime %s; последний этап: %s; adc workspace logs", state.Status, state.Progress.Stage)
		}
		if state.Progress.State == "ready" {
			return nil
		}
		if stage != state.Progress.Stage || time.Since(printed) >= heartbeat {
			stage, printed = state.Progress.Stage, time.Now()
			fmt.Fprintf(out, "[%s] Подготовка: %s\n", time.Since(started).Round(time.Second), strings.ReplaceAll(stage, "\n", " "))
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Ctrl+C ends the wait, but does not prove readiness. Container replacement
// must retain its recovery journal and previous container until ready is seen.
func (a *App) waitForWorkspaceReady(c shared.Config) (bool, error) {
	container := a.shared().Container(c)
	fmt.Printf("Контейнер %s работает в фоне. Подготовка может занять несколько минут.\n", container)
	fmt.Println("Команда вернёт консоль после подготовки. Ctrl+C прекращает только ожидание; контейнер продолжит работу.")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := waitWorkspace(ctx, func(ctx context.Context) (workspaceStartup, error) { return dockerStartup(ctx, container) }, os.Stdout, time.Second, 15*time.Second)
	if errors.Is(err, context.Canceled) {
		fmt.Println("Ожидание прервано. Контейнер продолжает работу; состояние: adc workspace status; прогресс: adc workspace logs.")
		return false, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false, errors.New("подготовка продолжается в фоне; adc workspace status покажет этап, adc workspace start повторно дождётся готовности")
	}
	if err != nil {
		return false, err
	}
	fmt.Printf("Готово. Workspace работает в фоне; консоль свободна. Orca → Add Server → 127.0.0.1:%d. Pairing: adc orca pairing --help\n", c.Port)
	return true, nil
}
