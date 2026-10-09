package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type MCPServer map[string]any
type MCPLayer struct {
	Scope   string         `json:"scope"`
	File    string         `json:"file"`
	Exists  bool           `json:"exists"`
	Servers map[string]any `json:"servers"`
}
type MCPOrigin struct {
	File    string   `json:"file,omitempty"`
	Scope   string   `json:"scope"`
	Shadows []string `json:"shadows"`
}
type MCPReason struct {
	Reason   string `json:"reason"`
	NeedEnv  string `json:"need_env,omitempty"`
	NeedPath string `json:"need_path,omitempty"`
}
type MCPPlan struct {
	RepoRoot string               `json:"repo_root"`
	Defs     map[string]MCPServer `json:"defs"`
	Layers   []MCPLayer           `json:"layers"`
	Origins  map[string]MCPOrigin `json:"origins"`
	Reasons  map[string]MCPReason `json:"reasons"`
	Disabled map[string]string    `json:"disabled"`
	Active   []string             `json:"active"`
	OAuth    []string             `json:"oauth"`
	resolved map[string]MCPServer
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var envPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
var mcpServerName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
var secretKey = regexp.MustCompile(`(?i)authorization|token|api[_-]?key|apikey|secret|password|passwd`)
var secretScheme = regexp.MustCompile(`^(Bearer|Basic|Token)\s+(\S{12,})$`)

func readLayer(scope, path string) (MCPLayer, error) {
	layer := MCPLayer{Scope: scope, File: path, Servers: map[string]any{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return layer, nil
	}
	if err != nil {
		return layer, err
	}
	layer.Exists = true
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return layer, fmt.Errorf("невалидный JSON: %s: %w", path, err)
	}
	if document == nil {
		return layer, fmt.Errorf("%s: требуется JSON объект", path)
	}
	if value, ok := document["mcpServers"]; ok && value != nil {
		servers, ok := value.(map[string]any)
		if !ok {
			return layer, fmt.Errorf("%s: mcpServers должен быть объектом", path)
		}
		for name, value := range servers {
			if !mcpServerName.MatchString(name) {
				return layer, fmt.Errorf("%s: недопустимое имя MCP %q", path, name)
			}
			if value != nil {
				if _, ok := value.(map[string]any); !ok {
					return layer, fmt.Errorf("%s: MCP %s должен быть объектом или null", path, name)
				}
			}
		}
		layer.Servers = servers
	}
	return layer, nil
}

// Parse values as data, never as shell code. Last occurrence of a key wins.
func readSecrets(path string) (map[string]string, error) {
	values := map[string]string{}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || !envName.MatchString(key) {
			continue
		}
		if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if _, ok := values[key]; ok {
			fmt.Fprintln(os.Stderr, path+": "+key+" задан больше одного раза; используется последняя строка")
		}
		values[key] = value
	}
	return values, scanner.Err()
}

func mergeObject(base, override map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range base {
		result[key] = cloneValue(value)
	}
	for key, value := range override {
		if strings.HasPrefix(key, "//") {
			continue
		}
		lower, lowerOK := result[key].(map[string]any)
		upper, upperOK := value.(map[string]any)
		if lowerOK && upperOK {
			result[key] = mergeObject(lower, upper)
		} else {
			result[key] = cloneValue(value)
		}
	}
	return result
}
func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return mergeObject(nil, value)
	case []any:
		items := make([]any, len(value))
		for index, item := range value {
			items[index] = cloneValue(item)
		}
		return items
	default:
		return value
	}
}
func expandValue(value any, variables map[string]string) any {
	switch value := value.(type) {
	case string:
		return envPlaceholder.ReplaceAllStringFunc(value, func(match string) string {
			key := envPlaceholder.FindStringSubmatch(match)[1]
			if value, ok := variables[key]; ok {
				return value
			}
			return match
		})
	case map[string]any:
		result := map[string]any{}
		for key, item := range value {
			result[key] = expandValue(item, variables)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			result[index] = expandValue(item, variables)
		}
		return result
	default:
		return value
	}
}
func literalSecret(key, value string) bool {
	return value != "" && !strings.Contains(value, "${") && (secretKey.MatchString(key) || secretScheme.MatchString(value))
}
func maskSecrets(value any, key string) any {
	switch value := value.(type) {
	case string:
		if literalSecret(key, value) {
			return "<redacted>"
		}
		return value
	case map[string]any:
		result := map[string]any{}
		for key, item := range value {
			result[key] = maskSecrets(item, key)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			result[index] = maskSecrets(item, key)
		}
		return result
	default:
		return value
	}
}

// Public plans contain declarations, never expanded tokens or literal secrets.
func (plan MCPPlan) MarshalJSON() ([]byte, error) {
	type plain MCPPlan
	data, err := json.Marshal(plain(plan))
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return json.Marshal(maskSecrets(value, ""))
}

func (a *App) mcpStore() string {
	if path := os.Getenv("AI_DEVCONTAINER_MCP_STORE"); path != "" {
		return path
	}
	return "/opt/ai-tools/share/mcp"
}
func (a *App) MCPPlan(p shared.Project) (MCPPlan, error) {
	plan := MCPPlan{RepoRoot: p.RuntimePath, Defs: map[string]MCPServer{}, Origins: map[string]MCPOrigin{}, Reasons: map[string]MCPReason{}, Disabled: map[string]string{}, Active: []string{}, OAuth: []string{}, resolved: map[string]MCPServer{}}
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return plan, err
	}
	paths := []string{a.shared().DeliveryPath(catalog.Base.MCP.Source), filepath.Join(a.mcpStore(), "servers.json"), filepath.Join(p.RuntimePath, ".agents", "mcp.json"), filepath.Join(p.RuntimePath, ".agents", "mcp.local.json")}
	scopes := []string{"global", "user", "project", "local"}
	allowed := map[string]bool{}
	for _, name := range catalog.Base.MCP.Servers {
		allowed[name] = true
	}
	capabilities, err := a.shared().ProjectCapabilities(p)
	if err != nil {
		return plan, err
	}
	for _, name := range capabilities {
		for _, server := range catalog.Capabilities[name].MCP {
			allowed[server] = true
		}
	}
	merged := map[string]any{}
	for index, path := range paths {
		layer, err := readLayer(scopes[index], path)
		if err != nil {
			return plan, err
		}
		if index == 0 {
			layer.Servers = map[string]any{}
			for name, definition := range catalog.MCPDefinitions {
				var value any
				if err := json.Unmarshal(definition, &value); err != nil {
					return plan, err
				}
				layer.Servers[name] = value
			}
			for name := range layer.Servers {
				if !allowed[name] {
					delete(layer.Servers, name)
				}
			}
		}
		plan.Layers = append(plan.Layers, layer)
		for _, name := range sortedKeys(layer.Servers) {
			origin := plan.Origins[name]
			if origin.Scope != "" {
				origin.Shadows = append(origin.Shadows, origin.Scope)
			}
			origin.Scope = layer.Scope
			origin.File = layer.File
			if index == 0 {
				origin.File = a.shared().DeliveryPath(catalog.MCPSources[name])
			}
			plan.Origins[name] = origin
		}
		merged = mergeObject(merged, layer.Servers)
	}
	variables := map[string]string{}
	for _, line := range os.Environ() {
		key, value, _ := strings.Cut(line, "=")
		variables[key] = value
	}
	for _, path := range []string{filepath.Join(a.mcpStore(), "secrets.env"), filepath.Join(p.RuntimePath, ".agents", "mcp.secrets.env")} {
		values, err := readSecrets(path)
		if err != nil {
			return plan, err
		}
		for key, value := range values {
			variables[key] = value
		}
	}
	variables["REPO_ROOT"] = p.RuntimePath
	for _, name := range sortedKeys(merged) {
		if merged[name] == nil {
			plan.Disabled[name] = plan.Origins[name].Scope
			continue
		}
		declaration := merged[name].(map[string]any)
		plan.Defs[name] = MCPServer(declaration)
		resolved := expandValue(declaration, variables).(map[string]any)
		requirements, _ := resolved["x-requires"].([]any)
		blocked := false
		for _, requirement := range requirements {
			req, ok := requirement.(string)
			if !ok {
				return plan, fmt.Errorf("MCP %s: x-requires должен содержать строки", name)
			}
			if key, ok := strings.CutPrefix(req, "env:"); ok && variables[key] == "" {
				plan.Reasons[name] = MCPReason{Reason: "нет переменной " + key, NeedEnv: key}
				blocked = true
				break
			}
			if path, ok := strings.CutPrefix(req, "path:"); ok {
				if _, err := os.Stat(path); err != nil {
					plan.Reasons[name] = MCPReason{Reason: "нет пути " + path, NeedPath: path}
					blocked = true
					break
				}
			}
		}
		if blocked {
			continue
		}
		if oauth, _ := resolved["x-oauth"].(bool); oauth {
			plan.OAuth = append(plan.OAuth, name)
		}
		delete(resolved, "x-requires")
		delete(resolved, "x-oauth")
		plan.Active = append(plan.Active, name)
		plan.resolved[name] = MCPServer(resolved)
	}
	return plan, nil
}
