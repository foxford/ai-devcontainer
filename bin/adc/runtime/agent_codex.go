package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type codexDriver struct{ defaultAgentBehavior }

func (codexDriver) Name() string { return "codex" }

type codexState struct {
	Headers []string `json:"headers"`
}

func decodeCodexState(previous agentState) (codexState, error) {
	var state codexState
	if len(previous.Data) > 0 {
		err := json.Unmarshal(previous.Data, &state)
		return state, err
	}
	if len(previous.Legacy) > 0 {
		var legacy struct {
			Headers []string `json:"codexHeaders"`
		}
		if err := json.Unmarshal(previous.Legacy, &legacy); err != nil {
			return state, err
		}
		state.Headers = legacy.Headers
	}
	return state, nil
}

func (codexDriver) Prepare(d agentDelivery) (agentPreparation, error) {
	previous, err := decodeCodexState(d.Previous)
	if err != nil {
		return agentPreparation{}, err
	}
	config := codexConfigPath(d.Home)
	headers := []string{}
	for _, name := range sortedKeys(d.Servers) {
		server := d.Servers[name]
		address, _ := server["url"].(string)
		values, _ := server["headers"].(map[string]any)
		kind := codexEntryKind(config, name)
		if address != "" && len(values) > 0 && !d.OAuth[name] && kind != "manual" && (shared.Contains(previous.Headers, name) || kind != "headers") {
			headers = append(headers, name)
		}
	}
	owned := append([]string{}, previous.Headers...)
	for _, name := range headers {
		if !shared.Contains(owned, name) {
			owned = append(owned, name)
		}
	}
	managed := map[string]bool{}
	add := func(names, ownedHeaders []string) {
		for _, name := range names {
			kind := codexEntryKind(config, name)
			if kind != "manual" && (kind != "headers" || shared.Contains(ownedHeaders, name)) {
				managed[name] = true
			}
		}
	}
	for _, checkout := range d.Registry {
		state, err := decodeCodexState(checkout.State)
		if err != nil {
			return agentPreparation{}, err
		}
		add(checkout.State.Servers, state.Headers)
	}
	add(d.Previous.Servers, previous.Headers)
	add(sortedKeys(d.Servers), owned)
	path := filepath.Join(d.Project.RuntimePath, ".codex", "config.toml")
	if _, err := codexProjectConfig(path, sortedKeys(d.Servers)); err != nil {
		return agentPreparation{}, err
	}
	if len(managed) > 0 {
		data, err := os.ReadFile(config)
		if err != nil && !os.IsNotExist(err) {
			return agentPreparation{}, err
		}
		if err := checkCodexScopeFormat(config, data); err != nil {
			return agentPreparation{}, err
		}
	}
	pending, err := json.Marshal(codexState{Headers: owned})
	if err != nil {
		return agentPreparation{}, err
	}
	final, err := json.Marshal(codexState{Headers: headers})
	if err != nil {
		return agentPreparation{}, err
	}
	return agentPreparation{
		PendingState: pending, FinalState: final,
		Commit: func() error {
			if err := disableCodexMCP(config, managed); err != nil {
				return err
			}
			if err := syncCodex(d.Servers, d.Previous.Servers, d.OAuth, owned); err != nil {
				return err
			}
			return scopeCodexMCP(d, path, managed)
		},
	}, nil
}

func codexConfigPath(home string) string {
	root := os.Getenv("CODEX_HOME")
	if root == "" {
		root = filepath.Join(home, ".codex")
	}
	return filepath.Join(root, "config.toml")
}

func codexEntryKind(path, name string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "none"
	}
	if codexInlineServers(string(data))[name] {
		return "manual"
	}
	found, headers, ours := false, false, false
	multiline := ""
	child := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if multiline == "" {
			if strings.HasPrefix(trimmed, "[") {
				var section string
				section, child = codexTableParts(trimmed)
				ours = section == name
				found = found || ours
			}
			if ours && (child == "http_headers" || child == "" && codexHeadersKey.MatchString(trimmed)) {
				headers = true
			}
		}
		multiline = codexStringState(line, multiline)
	}
	if headers {
		return "headers"
	}
	if found {
		return "other"
	}
	return "none"
}

func syncCodex(servers map[string]MCPServer, previous []string, oauth map[string]bool, ownedHeaders []string) error {
	binary, err := exec.LookPath("codex")
	if err != nil {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	config := codexConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		return err
	}
	// A project scope can temporarily reference a removed server during key
	// rotation. Administrative writes must not load that project's config.
	workdir, err := os.MkdirTemp("", "adc-codex-mcp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workdir)
	if err := os.Mkdir(filepath.Join(workdir, ".git"), 0700); err != nil {
		return err
	}
	call := func(args ...string) error {
		cmd := exec.Command(binary, args...)
		cmd.Dir = workdir
		cmd.Env = append(os.Environ(), "ADC_DELIVERY=1")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Codex MCP: %w", err)
		}
		if len(args) >= 3 && args[0] == "mcp" && args[1] == "add" {
			return disableCodexMCP(config, map[string]bool{args[2]: true})
		}
		return nil
	}
	for _, name := range previous {
		if _, ok := servers[name]; !ok {
			kind := codexEntryKind(config, name)
			if kind == "manual" || kind == "headers" && !shared.Contains(ownedHeaders, name) {
				continue
			}
			if kind != "none" {
				if err := call("mcp", "remove", name); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range sortedKeys(servers) {
		server := servers[name]
		if codexEntryKind(config, name) == "manual" {
			continue
		}
		if oauth[name] {
			fmt.Fprintln(os.Stderr, "Codex: OAuth MCP", name, "добавляется и авторизуется вручную")
			continue
		}
		args := []string{"mcp", "add", name}
		if url, _ := server["url"].(string); url != "" {
			if headers, ok := server["headers"].(map[string]any); ok && len(headers) > 0 {
				kind := codexEntryKind(config, name)
				if kind == "headers" && !shared.Contains(ownedHeaders, name) {
					// Retain an explicitly configured human entry.
					continue
				}
				if kind != "none" {
					if err := call("mcp", "remove", name); err != nil {
						return err
					}
				}
				if err := call("mcp", "add", name, "--url", url); err != nil {
					return err
				}
				data, err := os.ReadFile(config)
				if err != nil {
					return err
				}
				// Secret headers go directly into the private file, never CLI argv.
				quoted, _ := json.Marshal(name)
				var block strings.Builder
				fmt.Fprintf(&block, "\n[mcp_servers.%s.http_headers]\n", quoted)
				for _, key := range sortedKeys(headers) {
					value, ok := headers[key].(string)
					if !ok {
						return fmt.Errorf("MCP %s: HTTP header должен быть строкой", name)
					}
					k, _ := json.Marshal(key)
					v, _ := json.Marshal(value)
					fmt.Fprintf(&block, "%s = %s\n", k, v)
				}
				if err := secureWrite(config, append(data, block.String()...)); err != nil {
					return err
				}
				continue
			}
			args = append(args, "--url", url)
		} else {
			if env, ok := server["env"].(map[string]any); ok {
				for _, key := range sortedKeys(env) {
					args = append(args, "--env", key+"="+fmt.Sprint(env[key]))
				}
			}
			command, _ := server["command"].(string)
			if command == "" {
				continue
			}
			args = append(args, "--", command)
			if values, ok := server["args"].([]any); ok {
				for _, value := range values {
					args = append(args, fmt.Sprint(value))
				}
			}
		}
		if err := call(args...); err != nil {
			return err
		}
	}
	if _, err := os.Stat(config); err == nil {
		return os.Chmod(config, 0600)
	}
	return nil
}
