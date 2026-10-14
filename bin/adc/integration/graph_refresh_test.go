package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Delay a real Graphify update while keeping its version check and all graph
// generation intact. Marker files live in .adc, outside the source fingerprint.
func checkBackgroundGraphify(t *testing.T, root, tmp, cli, container, project string) {
	t.Helper()
	t.Log("checking sync, agent startup and Orca while a real Graphify update is blocked")
	run := func(binary string, args ...string) string {
		t.Helper()
		return graphifySmokeCommand(t, 30*time.Second, binary, args...)
	}
	execNode := func(args ...string) string {
		t.Helper()
		return run("docker", append([]string{"exec", "--user", "node", container}, args...)...)
	}
	graphify := strings.TrimSpace(execNode("sh", "-c", "command -v graphify"))
	if !filepath.IsAbs(graphify) {
		t.Fatalf("Graphify launcher is not an absolute path: %q", graphify)
	}
	realGraphify := graphify + "-adc-smoke-real"
	entered := filepath.Join(project, ".adc", "graphify-smoke-entered")
	release := filepath.Join(project, ".adc", "graphify-smoke-release")
	source := filepath.Join(tmp, "graphify-delay.go")
	proxy := filepath.Join(tmp, "graphify-delay")
	code := fmt.Sprintf(`package main
import (
 "fmt"
 "os"
 "os/exec"
 "time"
)
func main() {
 args := os.Args[1:]
 if len(args) > 1 && args[0] == "update" && args[1] == %q {
  if err := os.WriteFile(%q, []byte("entered\n"), 0644); err != nil { panic(err) }
  deadline := time.Now().Add(3 * time.Minute)
  for {
   if _, err := os.Stat(%q); err == nil { break }
   if time.Now().After(deadline) { fmt.Fprintln(os.Stderr, "Graphify smoke release timed out"); os.Exit(99) }
   time.Sleep(50 * time.Millisecond)
  }
 }
 command := exec.Command(%q, args...)
 command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
 if err := command.Run(); err != nil {
  if status, ok := err.(*exec.ExitError); ok { os.Exit(status.ExitCode()) }
  fmt.Fprintln(os.Stderr, err)
  os.Exit(1)
 }
}
`, project, entered, release, realGraphify)
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	buildCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	build := exec.CommandContext(buildCtx, "go", "build", "-o", proxy, source)
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := build.CombinedOutput()
	cancel()
	if err != nil {
		t.Fatalf("build Graphify delay fixture: %v %s", err, output)
	}
	execNode("mv", graphify, realGraphify)
	defer func() {
		// Release before restoring even after a failed assertion, so no fixture
		// process is left holding the checkout's index lock until its timeout.
		if err := os.WriteFile(release, []byte("release\n"), 0644); err != nil {
			t.Error(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "docker", "exec", "--user", "node", container, "mv", realGraphify, graphify).CombinedOutput(); err != nil {
			t.Errorf("restore Graphify launcher: %v %s", err, output)
		}
	}()
	run("docker", "cp", proxy, container+":"+graphify)
	run("docker", "exec", container, "chmod", "755", graphify)
	before := execNode("cat", "/tmp/adc-startup.json")
	var startup struct{ State string }
	decode(t, before, &startup)
	if startup.State != "ready" {
		t.Fatalf("workspace is not ready before background indexing: %s", before)
	}
	// Simulate the environment inherited by terminals of an Orca started during
	// initialization. Neither normal delivery nor its index worker owns ready.
	runtimeArgs := []string{"exec", "--user", "node", "--env", "ADC_INSTALL_AI_TOOLS=1", "--env", "ADC_STARTUP_STATUS=/tmp/adc-startup.json", "--workdir", project, container}
	syncProject := func(id string) {
		t.Helper()
		output := run("docker", append(append([]string{}, runtimeArgs...), "adc", "sync", id)...)
		if !strings.Contains(output, id+" OK") {
			t.Fatalf("project sync did not finish while indexing: %s", output)
		}
	}
	syncProject("app-a")
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(entered); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background Graphify update did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	syncProject("app-a")
	syncProject("app-b")
	if output := run("docker", append(append([]string{}, runtimeArgs...), "codex")...); !strings.Contains(output, "ADC_AGENT_STARTED") {
		t.Fatalf("agent did not start while Graphify was blocked: %s", output)
	}
	var status struct {
		Result struct{ Runtime struct{ Reachable bool } }
	}
	decode(t, run(cli, "orca", "status", "--json"), &status)
	if !status.Result.Runtime.Reachable {
		t.Fatal("Orca is unreachable while indexing")
	}
	if after := execNode("cat", "/tmp/adc-startup.json"); after != before {
		t.Fatalf("sync or agent hook rewrote startup readiness:\nbefore: %s\nafter: %s", before, after)
	}
	if status := run(cli, "workspace", "status"); !strings.Contains(status, "running (готов)") {
		t.Fatalf("indexing changed workspace readiness: %s", status)
	}
	if _, err := os.Stat(filepath.Join(project, "graphify-out", "graph.json")); !os.IsNotExist(err) {
		t.Fatalf("Graphify finished before its release: %v", err)
	}
	if err := os.WriteFile(release, []byte("release\n"), 0644); err != nil {
		t.Fatal(err)
	}
	waitForGraphify(t, container, project, "authenticate")
}

func graphifySmokeCommand(t *testing.T, timeout time.Duration, binary string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %q: %v (context: %v)\n%s", binary, args, err, ctx.Err(), output)
	}
	return string(output)
}

// A sync schedules work; only a complete graph and its committed fingerprint
// prove that its worker finished. This also supports volume-backed worktrees.
func waitForGraphify(t *testing.T, container, project, node string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var output []byte
	var lastErr error
	for ctx.Err() == nil {
		output, lastErr = exec.CommandContext(ctx, "docker", "exec", "--user", "node", container, "cat", filepath.Join(project, ".adc", "graphify-state.json"), filepath.Join(project, "graphify-out", "graph.json")).Output()
		if lastErr == nil {
			decoder := json.NewDecoder(strings.NewReader(string(output)))
			var state struct{ Fingerprint string }
			var graph json.RawMessage
			if decoder.Decode(&state) == nil && state.Fingerprint != "" && decoder.Decode(&graph) == nil && strings.Contains(string(graph), node) {
				return string(graph)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatalf("background Graphify did not finish for %s: %v (context: %v)\n%s", project, lastErr, ctx.Err(), output)
	return ""
}
