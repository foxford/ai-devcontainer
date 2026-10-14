package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

// The test binary acts as both a short-lived setup process and the Graphify CLI,
// so the actual worker launch, inherited handles and checkout locks are tested.
func graphBackgroundHelper() (int, bool) {
	if os.Getenv("ADC_GRAPH_BACKGROUND_HELPER") != "1" {
		return 0, false
	}
	args := os.Args[1:]
	if len(args) < 2 {
		return 2, true
	}
	a := &App{State: os.Getenv("ADC_HOME"), Platform: os.Getenv("AI_DEVCONTAINER_HOME"), Runtime: true}
	var err error
	switch args[0] {
	case "graphify-lock-fixture":
		_, err = a.graphLocker(args[1]).TryLocked(func() error {
			if err := os.WriteFile(filepath.Join(os.Getenv("ADC_GRAPH_BACKGROUND_CONTROL"), "locked"), []byte("locked"), 0600); err != nil {
				return err
			}
			time.Sleep(15 * time.Second)
			return nil
		})
	case "graphify-refresh":
		err = a.RefreshGraph(args[1])
		if done := os.Getenv("ADC_GRAPH_WORKER_DONE"); done != "" {
			_ = os.WriteFile(done, []byte(fmt.Sprint(err)), 0600)
		}
	case "graphify-setup-fixture":
		var p shared.Project
		p, err = a.shared().CapabilityProject(args[1])
		if err == nil {
			a.syncGraph(p)
			fmt.Println("setup returned")
		}
	case "update":
		control := os.Getenv("ADC_GRAPH_BACKGROUND_CONTROL")
		fmt.Fprintln(os.Stderr, "ADC_GRAPH_INDEX_ACTIVE")
		err = os.WriteFile(filepath.Join(control, "started"), []byte("started"), 0600)
		if err == nil {
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, statErr := os.Stat(filepath.Join(control, "release")); statErr == nil {
					break
				}
				if time.Now().After(deadline) {
					return 3, true
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, statErr := os.Stat(filepath.Join(control, "fail")); statErr == nil {
				fmt.Fprintln(os.Stderr, "ADC_GRAPH_INDEX_FAILURE")
				return 9, true
			}
			err = os.MkdirAll(filepath.Join(args[1], "graphify-out"), 0755)
			if err == nil {
				err = os.WriteFile(filepath.Join(args[1], "graphify-out", "graph.json"), []byte(`{"nodes":[{"id":"background"}]}`), 0644)
			}
		}
	default:
		return 2, true
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	return 0, true
}

func graphBackgroundFixture(t *testing.T) (*App, shared.Project, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("runtime registry and background indexing use Linux checkout paths")
	}
	testutil.Home(t)
	root := graphRepository(t)
	a := &App{State: t.TempDir(), Platform: testutil.Root(t), Runtime: true}
	p := shared.Project{ID: "app", Path: root, RuntimePath: root, Capabilities: []string{"graphify"}}
	if err := a.shared().Save(shared.Config{Version: shared.Schema, Name: "fixture", Port: 6768, Projects: []shared.Project{p}}); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	name := "graphify"
	if filepath.Ext(binary) == ".exe" {
		name += ".exe"
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), data, 0755); err != nil {
		t.Fatal(err)
	}
	control := t.TempDir()
	for key, value := range map[string]string{
		"PATH":        directory + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ADC_RUNTIME": "1", "ADC_INSTALL_AI_TOOLS": "1", "ADC_HOME": a.State, "AI_DEVCONTAINER_HOME": a.Platform,
		"ADC_GRAPH_BACKGROUND_HELPER": "1", "ADC_GRAPH_BACKGROUND_CONTROL": control,
		"ADC_GRAPH_WORKER_DONE": filepath.Join(control, "worker-done"),
	} {
		t.Setenv(key, value)
	}
	return a, p, control
}

func waitGraphBackgroundFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background worker did not create %s", path)
}

func TestGraphBackgroundSurvivesSetupExitWithoutHoldingOutputPipes(t *testing.T) {
	a, p, control := graphBackgroundFixture(t)
	t.Cleanup(func() {
		writeFixture(t, filepath.Join(control, "release"), "release")
		waitGraphBackgroundFile(t, filepath.Join(control, "worker-done"))
	})
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "graphify-setup-fixture", p.RuntimePath)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "setup returned") {
		t.Fatalf("setup waited for indexing or retained worker output pipes: %v\n%s", err, output)
	}
	waitGraphBackgroundFile(t, filepath.Join(control, "started"))
	if _, err := os.Stat(filepath.Join(control, "worker-done")); !os.IsNotExist(err) {
		t.Fatal("worker stopped with setup instead of continuing index work", err)
	}
	// Another automatic refresh must exit while the first indexer is blocked.
	duplicate := exec.CommandContext(ctx, binary, "graphify-refresh", p.RuntimePath)
	duplicate.Env = replaceEnv(os.Environ(), map[string]string{"ADC_GRAPH_WORKER_DONE": ""})
	if output, err := duplicate.CombinedOutput(); err != nil {
		t.Fatalf("duplicate worker waited for active indexer: %v\n%s", err, output)
	}
	if !strings.Contains(readFixture(t, graphLogPath(p.RuntimePath)), "ADC_GRAPH_INDEX_ACTIVE") {
		t.Fatal("duplicate worker truncated the active indexer's log")
	}
	writeFixture(t, filepath.Join(control, "release"), "release")
	waitGraphBackgroundFile(t, filepath.Join(control, "worker-done"))
	if message := readFixture(t, filepath.Join(control, "worker-done")); message != "<nil>" {
		t.Fatal("background indexing failed after setup exited:", message)
	}
	if _, err := os.Stat(filepath.Join(p.RuntimePath, shared.ProjectStateDir, "graphify-state.json")); err != nil {
		t.Fatal("background refresh did not commit its fingerprint", err)
	}
	acquired, err := a.graphLocker(p.RuntimePath).TryLocked(func() error { return nil })
	if err != nil || !acquired {
		t.Fatal("completed worker retained its checkout lock", acquired, err)
	}
}

func TestGraphBackgroundFailureReleasesLockAndCanRetry(t *testing.T) {
	a, p, control := graphBackgroundFixture(t)
	writeFixture(t, filepath.Join(control, "release"), "release")
	writeFixture(t, filepath.Join(control, "fail"), "fail")
	if err := a.startGraphRefresh(p.RuntimePath); err != nil {
		t.Fatal(err)
	}
	waitGraphBackgroundFile(t, filepath.Join(control, "worker-done"))
	if message := readFixture(t, filepath.Join(control, "worker-done")); !strings.Contains(message, "индекс не обновлён") {
		t.Fatal("failure was lost:", message)
	}
	if _, err := os.Stat(filepath.Join(p.RuntimePath, shared.ProjectStateDir, "graphify-state.json")); !os.IsNotExist(err) {
		t.Fatal("failed refresh committed a successful fingerprint", err)
	}
	if err := os.Remove(filepath.Join(control, "fail")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(control, "worker-done")); err != nil {
		t.Fatal(err)
	}
	if err := a.startGraphRefresh(p.RuntimePath); err != nil {
		t.Fatal(err)
	}
	waitGraphBackgroundFile(t, filepath.Join(control, "worker-done"))
	if message := readFixture(t, filepath.Join(control, "worker-done")); message != "<nil>" {
		t.Fatal("failed worker kept its lock or retry failed:", message)
	}
	if _, err := os.Stat(filepath.Join(p.RuntimePath, shared.ProjectStateDir, "graphify-state.json")); err != nil {
		t.Fatal("retry did not commit successful fingerprint", err)
	}
}

func TestGraphBackgroundCheckoutLockIsReleasedAfterProcessExit(t *testing.T) {
	a := &App{State: t.TempDir()}
	root, control := t.TempDir(), t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "graphify-lock-fixture", root)
	cmd.Env = replaceEnv(os.Environ(), map[string]string{
		"ADC_GRAPH_BACKGROUND_HELPER": "1", "ADC_HOME": a.State, "ADC_GRAPH_BACKGROUND_CONTROL": control,
	})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	waitGraphBackgroundFile(t, filepath.Join(control, "locked"))
	acquired, err := a.graphLocker(root).TryLocked(func() error { return nil })
	if err != nil || acquired {
		t.Fatal("child process did not retain its checkout lock", acquired, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker process did not exit")
	}
	acquired, err = a.graphLocker(root).TryLocked(func() error { return nil })
	if err != nil || !acquired {
		t.Fatal("terminated worker retained its checkout lock", acquired, err)
	}
}

func TestGraphBackgroundIgnoresReadOnlyDisabledAndUnregisteredProjects(t *testing.T) {
	for _, scenario := range []string{"readonly", "disabled", "tools-disabled", "unregistered"} {
		t.Run(scenario, func(t *testing.T) {
			a, p, _ := graphBackgroundFixture(t)
			switch scenario {
			case "readonly":
				p.ReadOnly = true
			case "disabled":
				p.Capabilities = nil
			case "tools-disabled":
				t.Setenv("ADC_INSTALL_AI_TOOLS", "0")
			}
			projects := []shared.Project{p}
			if scenario == "unregistered" {
				projects = nil
			}
			if err := a.shared().Save(shared.Config{Version: shared.Schema, Name: "fixture", Port: 6768, Projects: projects}); err != nil {
				t.Fatal(err)
			}
			err := a.RefreshGraph(p.RuntimePath)
			if (err != nil) != (scenario == "unregistered") {
				t.Fatal("wrong refresh result", err)
			}
			for _, dir := range []string{shared.ProjectStateDir, "graphify-out"} {
				if _, err := os.Stat(filepath.Join(p.RuntimePath, dir)); !os.IsNotExist(err) {
					t.Fatal("worker changed an excluded checkout", dir, err)
				}
			}
		})
	}
}
