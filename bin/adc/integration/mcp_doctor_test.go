package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real runtime transport fixture; no vault, model account or network service.
func TestDockerMCPHTTPFixture(t *testing.T) {
	if os.Getenv("ADC_MCP_HTTP_FIXTURE") != "1" {
		return
	}
	_ = http.ListenAndServe("127.0.0.1:27791", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer adc-fixture-key" {
			w.WriteHeader(401)
			return
		}
		var request struct {
			ID     int
			Method string
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		if request.ID == 0 {
			w.WriteHeader(202)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "runtime-fixture", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "read_vault"}}}
		default:
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
}

// Minimal native config CLI used only by the smoke workspace's agent adapter.
func TestDockerCodexConfigFixture(t *testing.T) {
	if os.Getenv("ADC_CODEX_CONFIG_FIXTURE") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if len(args) < 3 || args[0] != "mcp" {
		fmt.Println("ADC_AGENT_STARTED")
		return
	}
	path := filepath.Join("/home/node", ".codex", "config.toml")
	data, _ := os.ReadFile(path)
	name := args[2]
	prefix := "mcp_servers." + name
	quoted := "mcp_servers.\"" + name + "\""
	lines := []string{}
	skip := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "[") {
			section := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			skip = section == prefix || strings.HasPrefix(section, prefix+".") || section == quoted || strings.HasPrefix(section, quoted+".")
		}
		if !skip {
			lines = append(lines, line)
		}
	}
	content := strings.Join(lines, "\n")
	if args[1] == "add" {
		q, _ := json.Marshal(name)
		content += fmt.Sprintf("\n[mcp_servers.%s]\n", q)
		if len(args) > 4 && args[3] == "--url" {
			address, _ := json.Marshal(args[4])
			content += fmt.Sprintf("url = %s\n", address)
		} else {
			content += "command = \"fixture\"\n"
		}
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
