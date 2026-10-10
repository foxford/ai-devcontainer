package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func graphRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, data)
	}
	writeFixture(t, filepath.Join(root, ".gitignore"), "graphify-out/\n.ai-devcontainer/\n")
	writeFixture(t, filepath.Join(root, "main.go"), "package main\nfunc Alpha() {}\n")
	return root
}
func TestGraphIndexTracksDirtyFilesDeletionAndFailures(t *testing.T) {
	root := graphRepository(t)
	calls := 0
	run := func(dir, name string, args ...string) error {
		calls++
		if dir != root || name != "graphify" || strings.Join(args, " ") != "update "+root+" --force" {
			t.Fatal("wrong index scope", args)
		}
		writeFixture(t, filepath.Join(root, "graphify-out", "graph.json"), `{"nodes":[{"id":"alpha"}]}`)
		return nil
	}
	ensure := func() {
		t.Helper()
		if err := EnsureGraph(root, false, run, io.Discard, "0.9.82"); err != nil {
			t.Fatal(err)
		}
	}
	ensure()
	ensure()
	if calls != 1 {
		t.Fatal("unchanged checkout rebuilt")
	}
	writeFixture(t, filepath.Join(root, "main.go"), "package main\nfunc Bravo() {}\n")
	ensure()
	if calls != 2 {
		t.Fatal("dirty source change missed")
	}
	writeFixture(t, filepath.Join(root, "new.go"), "package main\nfunc Gamma() {}\n")
	ensure()
	if err := os.Remove(filepath.Join(root, "new.go")); err != nil {
		t.Fatal(err)
	}
	ensure()
	if calls != 4 {
		t.Fatal("untracked add/delete missed")
	}
	writeFixture(t, filepath.Join(root, ".graphifyignore"), "ignored/\n")
	ensure()
	if calls != 5 {
		t.Fatal("ignore change missed")
	}
	checkpoint := readFixture(t, filepath.Join(root, shared.ProjectStateDir, "graphify-state.json"))
	writeFixture(t, filepath.Join(root, "main.go"), "package main\nfunc Delta() {}\n")
	if err := EnsureGraph(root, false, func(string, string, ...string) error { return errors.New("index failed") }, io.Discard, "0.9.82"); err == nil {
		t.Fatal("failure hidden")
	}
	if readFixture(t, filepath.Join(root, shared.ProjectStateDir, "graphify-state.json")) != checkpoint {
		t.Fatal("failed index marked current")
	}
	ensure()
	if calls != 6 {
		t.Fatal("failed index not retried")
	}
	if err := EnsureGraph(root, false, run, io.Discard, "new-package-version"); err != nil {
		t.Fatal(err)
	}
	if calls != 7 {
		t.Fatal("package update did not rebuild")
	}
}
func TestGraphIndexRejectsForeignOutputAndMalformedGraph(t *testing.T) {
	requireSymlinks(t)
	root := graphRepository(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "graphify-out")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGraph(root, false, func(string, string, ...string) error { t.Fatal("foreign directory used"); return nil }, io.Discard); err == nil {
		t.Fatal("foreign output accepted")
	}
	if err := os.Remove(filepath.Join(root, "graphify-out")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGraph(root, false, func(string, string, ...string) error {
		writeFixture(t, filepath.Join(root, "graphify-out", "graph.json"), `{"wrong":true}`)
		return nil
	}, io.Discard); err == nil {
		t.Fatal("invalid graph marked current")
	}
	if _, err := os.Stat(filepath.Join(root, shared.ProjectStateDir, "graphify-state.json")); !os.IsNotExist(err) {
		t.Fatal("bad graph checkpoint written")
	}
}
func TestGraphifySkillIsScopedAndRemovedWithCapability(t *testing.T) {
	requireSymlinks(t)
	a := &App{Platform: testutil.Delivery(t), Runtime: true}
	root := t.TempDir()
	if err := a.DeliverSkills(root, nil); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".claude", "skills", "graphify", "SKILL.md")
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("optional skill delivered globally")
	}
	if err := a.DeliverSkills(root, []string{"graphify"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFixture(t, target), "graphify_query") {
		t.Fatal("bundle skill absent")
	}
	fork := filepath.Join(root, ".agents", "skills", "graphify", "SKILL.md")
	writeFixture(t, fork, "---\nname: graphify\n---\nproject graph rules\n")
	if err := a.DeliverSkills(root, []string{"graphify"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFixture(t, target), "project graph rules") {
		t.Fatal("fork lost")
	}
	if err := os.RemoveAll(filepath.Dir(fork)); err != nil {
		t.Fatal(err)
	}
	if err := a.DeliverSkills(root, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("inactive skill remained")
	}
}
func TestGraphMCPProtocolAndErrors(t *testing.T) {
	requests := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"graphify_query","arguments":{"question":"architecture"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"graphify_explain","arguments":{"node":"missing"}}}
{"jsonrpc":"2.0","id":5,"method":"unknown"}
invalid JSON
`
	var output bytes.Buffer
	err := serveGraphMCP(strings.NewReader(requests), &output, func(name string, args map[string]string) (string, error) {
		if name == "graphify_explain" {
			return "", errors.New("fixture failure")
		}
		return "found source: main.go", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	responses := []map[string]any{}
	for decoder.More() {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 6 {
		t.Fatalf("unexpected responses: %v", responses)
	}
	initialized := responses[0]["result"].(map[string]any)
	if initialized["protocolVersion"] != "2025-06-18" {
		t.Fatal("bad handshake")
	}
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Fatal("missing graph tools")
	}
	success := responses[2]["result"].(map[string]any)
	failure := responses[3]["result"].(map[string]any)
	if success["isError"] != false || failure["isError"] != true {
		t.Fatal("tool failure protocol")
	}
	if responses[4]["error"].(map[string]any)["code"] != float64(-32601) || responses[5]["error"].(map[string]any)["code"] != float64(-32700) {
		t.Fatal("RPC errors incorrect")
	}
}
func TestGraphMCPArgumentsCannotReplaceCheckout(t *testing.T) {
	for _, name := range []string{"query", "explain", "path", "affected"} {
		values := map[string]string{"node": "Alpha"}
		if name == "query" {
			values = map[string]string{"question": "who uses Alpha"}
		}
		if name == "path" {
			values = map[string]string{"from": "Alpha", "to": "Beta"}
		}
		args, err := graphArguments("graphify_"+name, values, "/project-a")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(args, " "), "--graph "+filepath.Join("/project-a", "graphify-out", "graph.json")) {
			t.Fatal("checkout graph absent", args)
		}
	}
	for _, values := range []map[string]string{{"question": "--graph"}, {"question": "architecture", "graph": "/project-b/graph.json"}, {"question": ""}} {
		if _, err := graphArguments("graphify_query", values, "/project-a"); err == nil {
			t.Fatal("unsafe tool input accepted")
		}
	}
}
