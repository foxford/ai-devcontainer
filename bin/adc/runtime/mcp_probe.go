package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const probeLimit = 2 << 20

type probeReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// Probe only discovers tools; it never calls a tool or prints server bodies.
func probeMCP(ctx context.Context, root string, server MCPServer) (int, error) {
	if address, _ := server["url"].(string); address != "" {
		return probeHTTP(ctx, address, server)
	}
	command, _ := server["command"].(string)
	if command == "" {
		return 0, errors.New("нет команды или HTTP URL")
	}
	args := []string{}
	if values, ok := server["args"].([]any); ok {
		for _, value := range values {
			args = append(args, fmt.Sprint(value))
		}
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = root
	if cwd, _ := server["cwd"].(string); cwd != "" {
		cmd.Dir = cwd
	}
	env := map[string]string{"ADC_DELIVERY": "1"}
	if values, ok := server["env"].(map[string]any); ok {
		for key, value := range values {
			env[key] = fmt.Sprint(value)
		}
	}
	cmd.Env = replaceEnv(os.Environ(), env)
	cmd.WaitDelay = time.Second
	input, err := cmd.StdinPipe()
	if err != nil {
		return 0, errors.New("не удалось открыть stdin MCP")
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return 0, errors.New("не удалось открыть stdout MCP")
	}
	if err := cmd.Start(); err != nil {
		return 0, errors.New("команда MCP не запускается; проверьте executable и зависимости")
	}
	defer func() { input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), probeLimit)
	messages := make(chan []byte)
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		defer close(messages)
		for scanner.Scan() {
			data := append([]byte(nil), scanner.Bytes()...)
			select {
			case messages <- data:
			case <-stopped:
				return
			}
		}
	}()
	exchange := func(method string, id int, params any) (json.RawMessage, error) {
		request := map[string]any{"jsonrpc": "2.0", "method": method}
		if id != 0 {
			request["id"] = id
		}
		if params != nil {
			request["params"] = params
		}
		if err := json.NewEncoder(input).Encode(request); err != nil {
			return nil, errors.New("MCP завершился при отправке запроса")
		}
		if id == 0 {
			return nil, nil
		}
		for {
			select {
			case <-ctx.Done():
				return nil, errors.New("таймаут MCP")
			case data, ok := <-messages:
				if !ok {
					return nil, errors.New("MCP закрыл stdout до ответа; проверьте запуск сервера")
				}
				result, matched, err := parseProbeReply(data, id)
				if matched || err != nil {
					return result, err
				}
			}
		}
	}
	return probeHandshake(exchange)
}

func validMCPURL(address string) error {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return errors.New("нужен HTTP/HTTPS URL MCP без логина, ключа, query и fragment")
	}
	return nil
}

func probeHTTP(ctx context.Context, address string, server MCPServer) (int, error) {
	if err := validMCPURL(address); err != nil {
		return 0, err
	}
	// Never forward authorization to a redirect destination.
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	session, protocol := "", ""
	exchange := func(method string, id int, params any) (json.RawMessage, error) {
		request := map[string]any{"jsonrpc": "2.0", "method": method}
		if id != 0 {
			request["id"] = id
		}
		if params != nil {
			request["params"] = params
		}
		data, _ := json.Marshal(request)
		req, err := http.NewRequestWithContext(ctx, "POST", address, bytes.NewReader(data))
		if err != nil {
			return nil, errors.New("не удалось создать запрос MCP")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if headers, ok := server["headers"].(map[string]any); ok {
			for key, value := range headers {
				req.Header.Set(key, fmt.Sprint(value))
			}
		}
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		if protocol != "" {
			req.Header.Set("MCP-Protocol-Version", protocol)
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, errors.New("таймаут MCP; проверьте адрес, плагин и доступ из контейнера")
			}
			return nil, errors.New("MCP недоступен; проверьте адрес, TLS сертификат, плагин и доступ из контейнера")
		}
		defer resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, errors.New("HTTP 401/403: проверьте API key и права доступа")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d: проверьте путь /mcp/ и настройки сервера", resp.StatusCode)
		}
		if value := resp.Header.Get("Mcp-Session-Id"); value != "" {
			session = value
		}
		if id == 0 {
			return nil, nil
		}
		var result json.RawMessage
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			scanner := bufio.NewScanner(io.LimitReader(resp.Body, probeLimit+1))
			scanner.Buffer(make([]byte, 4096), probeLimit)
			var event strings.Builder
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data:") {
					event.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
					event.WriteByte('\n')
				}
				if line == "" && event.Len() > 0 {
					parsed, matched, err := parseProbeReply([]byte(event.String()), id)
					if err != nil {
						return nil, err
					}
					if matched {
						result = parsed
						break
					}
					event.Reset()
				}
			}
			if result == nil {
				return nil, errors.New("MCP не вернул ожидаемый SSE ответ")
			}
		} else {
			data, err := io.ReadAll(io.LimitReader(resp.Body, probeLimit+1))
			if err != nil || len(data) > probeLimit {
				return nil, errors.New("ответ MCP не прочитан или слишком велик")
			}
			var matched bool
			result, matched, err = parseProbeReply(data, id)
			if err != nil {
				return nil, err
			}
			if !matched {
				return nil, errors.New("MCP вернул неверный id ответа")
			}
		}
		if method == "initialize" {
			var init struct {
				Protocol string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(result, &init)
			protocol = init.Protocol
		}
		return result, nil
	}
	count, err := probeHandshake(exchange)
	if session != "" {
		// Session cleanup does not extend the diagnostic timeout.
		req, _ := http.NewRequestWithContext(ctx, "DELETE", address, nil)
		req.Header.Set("Mcp-Session-Id", session)
		if headers, ok := server["headers"].(map[string]any); ok {
			for key, value := range headers {
				req.Header.Set(key, fmt.Sprint(value))
			}
		}
		if protocol != "" {
			req.Header.Set("MCP-Protocol-Version", protocol)
		}
		if resp, e := client.Do(req); e == nil {
			resp.Body.Close()
		}
	}
	return count, err
}

func parseProbeReply(data []byte, id int) (json.RawMessage, bool, error) {
	var reply probeReply
	if json.Unmarshal(data, &reply) != nil || reply.JSONRPC != "2.0" {
		return nil, false, errors.New("невалидный JSON-RPC ответ MCP")
	}
	if string(reply.ID) != fmt.Sprint(id) {
		return nil, false, nil
	}
	if reply.Error != nil {
		return nil, true, fmt.Errorf("MCP вернул JSON-RPC ошибку %d", reply.Error.Code)
	}
	if len(reply.Result) == 0 || string(reply.Result) == "null" {
		return nil, true, errors.New("MCP не вернул result")
	}
	return reply.Result, true, nil
}

func probeHandshake(exchange func(string, int, any) (json.RawMessage, error)) (int, error) {
	result, err := exchange("initialize", 1, map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "adc-doctor", "version": "1"}})
	if err != nil {
		return 0, err
	}
	var init struct {
		Protocol     string                     `json:"protocolVersion"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
		Server       json.RawMessage            `json:"serverInfo"`
	}
	if json.Unmarshal(result, &init) != nil || init.Protocol == "" || init.Capabilities == nil || len(init.Server) == 0 {
		return 0, errors.New("невалидный initialize MCP")
	}
	if _, err := exchange("notifications/initialized", 0, nil); err != nil {
		return 0, err
	}
	if _, ok := init.Capabilities["tools"]; !ok {
		return 0, errors.New("MCP не объявляет поддержку tools")
	}
	count, cursor := 0, ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, err = exchange("tools/list", page+2, params)
		if err != nil {
			return 0, err
		}
		var list struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Cursor string `json:"nextCursor"`
		}
		if json.Unmarshal(result, &list) != nil || list.Tools == nil {
			return 0, errors.New("невалидный tools/list MCP")
		}
		for _, tool := range list.Tools {
			if tool.Name == "" {
				return 0, errors.New("MCP вернул инструмент без имени")
			}
		}
		count += len(list.Tools)
		cursor = list.Cursor
		if cursor == "" {
			return count, nil
		}
		if seen[cursor] {
			return 0, errors.New("повторяющийся cursor MCP")
		}
		seen[cursor] = true
	}
	return 0, errors.New("слишком много страниц tools/list MCP")
}
