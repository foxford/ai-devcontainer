package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func (a *App) Serve() (result error) {
	if err := a.shared().RequireRuntime(); err != nil {
		return err
	}
	if err := runtimeUser(); err != nil {
		return err
	}
	if err := os.Setenv("ADC_STARTUP_STATUS", shared.StartupPath); err != nil {
		return err
	}
	defer func() {
		if result != nil {
			shared.StartupFinished(result)
		}
	}()
	shared.StartupProgress("Запуск сервера Orca")
	if err := os.Remove("/tmp/adc-ready"); err != nil && !os.IsNotExist(err) {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	port := os.Getenv("ADC_ORCA_PORT")
	if port == "" {
		port = "6768"
	}
	orca := exec.Command("orca", "serve", "--port", "6768", "--pairing-address", "127.0.0.1:"+port, "--json")
	orca.Stdout, orca.Stderr = os.Stdout, os.Stderr
	if err := orca.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- orca.Wait(); close(done) }()
	defer func() {
		_ = orca.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = orca.Process.Kill()
			<-done
		}
	}()
	probe := func(ctx context.Context) bool {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		data, err := exec.CommandContext(probeCtx, "orca", "status", "--json").Output()
		if err != nil {
			return false
		}
		var status struct {
			Result struct{ Runtime struct{ Reachable bool } }
		}
		return json.Unmarshal(data, &status) == nil && status.Result.Runtime.Reachable
	}
	if err := waitRuntime(ctx, done, probe, 120*time.Second); err != nil {
		return err
	}
	shared.StartupProgress("Orca отвечает; подготовка среды")
	initDone := make(chan error, 1)
	go func() { initDone <- a.Init() }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return stoppedOrca(err)
	case err := <-initDone:
		if err != nil {
			return err
		}
	}
	shared.StartupFinished(nil)
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return stoppedOrca(err)
	}
}

func stoppedOrca(err error) error {
	if err == nil {
		return errors.New("сервер Orca завершился")
	}
	return fmt.Errorf("сервер Orca: %w", err)
}

func waitRuntime(ctx context.Context, done <-chan error, probe func(context.Context) bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ожидание Orca: %w", ctx.Err())
		case err := <-done:
			return stoppedOrca(err)
		default:
		}
		if probe(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ожидание Orca: %w", ctx.Err())
		case err := <-done:
			return stoppedOrca(err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
