package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type graphRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type graphTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"inputSchema"`
}

func graphTools() []graphTool {
	tools := []graphTool{}
	for _, entry := range []struct {
		name, description string
		fields            []string
	}{{"query", "Найти связи и исходники по вопросу об архитектуре", []string{"question"}}, {"explain", "Показать узел и его связи", []string{"node"}}, {"path", "Найти путь между двумя узлами", []string{"from", "to"}}, {"affected", "Найти узлы, зависящие от изменяемого узла", []string{"node"}}} {
		properties := map[string]any{}
		for _, field := range entry.fields {
			properties[field] = map[string]any{"type": "string", "minLength": 1, "maxLength": 4096}
		}
		tools = append(tools, graphTool{"graphify_" + entry.name, entry.description, map[string]any{"type": "object", "properties": properties, "required": entry.fields, "additionalProperties": false}})
	}
	return tools
}
func graphArguments(name string, values map[string]string, root string) ([]string, error) {
	fields := map[string][]string{"graphify_query": {"question"}, "graphify_explain": {"node"}, "graphify_path": {"from", "to"}, "graphify_affected": {"node"}}
	keys, ok := fields[name]
	if !ok {
		return nil, errors.New("неизвестный инструмент Graphify")
	}
	if len(values) != len(keys) {
		return nil, errors.New("неверные аргументы Graphify")
	}
	args := []string{strings.TrimPrefix(name, "graphify_")}
	for _, key := range keys {
		value := strings.TrimSpace(values[key])
		if value == "" || len(value) > 4096 || strings.HasPrefix(value, "-") {
			return nil, fmt.Errorf("неверное поле %s", key)
		}
		args = append(args, value)
	}
	args = append(args, "--graph", filepath.Join(root, "graphify-out", "graph.json"))
	if name == "graphify_query" {
		args = append(args, "--budget", "1500")
	}
	return args, nil
}

func (a *App) GraphMCP(root string, input io.Reader, output io.Writer) error {
	if err := a.shared().RequireRuntime(); err != nil {
		return err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	checkout, err := shared.Output(absolute, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if filepath.Clean(absolute) != filepath.Clean(checkout) {
		return errors.New("MCP требует корень checkout")
	}
	root = checkout
	config, err := a.shared().Load()
	if err != nil {
		return err
	}
	project, err := a.FindCheckout(config, root)
	if err != nil {
		return err
	}
	project.RuntimePath = root
	caps, err := a.shared().ProjectCapabilities(project)
	if err != nil {
		return err
	}
	if !shared.Contains(caps, "graphify") {
		return errors.New("checkout не подключил capability graphify")
	}
	call := func(name string, values map[string]string) (string, error) {
		args, err := graphArguments(name, values, root)
		if err != nil {
			return "", err
		}
		if !project.ReadOnly {
			if err = a.ensureGraph(root, false, io.Discard); err != nil {
				return "", err
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "graphify", args...)
		cmd.Dir = root
		cmd.Env = replaceEnv(installerEnv("/home/node", toolsHome()), map[string]string{"GRAPHIFY_NO_AUTO_REFRESH": "1"})
		data, err := cmd.Output()
		return string(data), err
	}
	return serveGraphMCP(input, output, call)
}
func serveGraphMCP(input io.Reader, output io.Writer, call func(string, map[string]string) (string, error)) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	write := func(id json.RawMessage, result any, code int, message string) error {
		response := map[string]any{"jsonrpc": "2.0", "id": id}
		if code != 0 {
			response["error"] = map[string]any{"code": code, "message": message}
		} else {
			response["result"] = result
		}
		return json.NewEncoder(output).Encode(response)
	}
	for scanner.Scan() {
		var request graphRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			if err = write(json.RawMessage("null"), nil, -32700, "Parse error"); err != nil {
				return err
			}
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any
		code := 0
		message := ""
		if request.JSONRPC != "2.0" {
			code = -32600
			message = "Invalid Request"
		} else {
			switch request.Method {
			case "initialize":
				var params struct{ ProtocolVersion string }
				_ = json.Unmarshal(request.Params, &params)
				if params.ProtocolVersion == "" {
					params.ProtocolVersion = "2024-11-05"
				}
				result = map[string]any{"protocolVersion": params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "adc-graphify", "version": "1.0.0"}, "instructions": "Use the checkout graph for architecture and impact analysis, then verify relevant source files."}
			case "ping":
				result = map[string]any{}
			case "tools/list":
				result = map[string]any{"tools": graphTools()}
			case "tools/call":
				var params struct {
					Name      string
					Arguments map[string]string
				}
				if err := json.Unmarshal(request.Params, &params); err != nil {
					code = -32602
					message = "Invalid params"
					break
				}
				text, err := call(params.Name, params.Arguments)
				if err != nil {
					text = "Graphify недоступна: " + err.Error() + ". Используйте поиск и исходники."
				}
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil}
			default:
				code = -32601
				message = "Method not found"
			}
		}
		if err := write(request.ID, result, code, message); err != nil {
			return err
		}
	}
	return scanner.Err()
}
