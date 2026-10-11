package shared

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type DeliveryBundle struct {
	Version     int                   `json:"version"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Conflicts   []string              `json:"conflicts,omitempty"`
	Activation  *CapabilityActivation `json:"activation,omitempty"`
	Exports     struct {
		Instructions []string           `json:"instructions"`
		Skills       []string           `json:"skills,omitempty"`
		Documents    []DeliveryDocument `json:"documents,omitempty"`
		Tools        []DeliveryTool     `json:"tools,omitempty"`
		MCP          struct {
			Source  string   `json:"source"`
			Servers []string `json:"servers"`
		} `json:"mcp"`
	} `json:"exports"`
}

// Activation describes runtime configuration questions, without secret values.
type CapabilityActivation struct {
	Server       string             `json:"server"`
	Requirements []string           `json:"requirements"`
	Prompts      []ActivationPrompt `json:"prompts"`
}
type ActivationPrompt struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Default string `json:"default,omitempty"`
	Secret  bool   `json:"secret,omitempty"`
}

// The catalog stores registrations, while the resolved capability is consumed by
// the same delivery engines as ordinary capabilities. Sources stay bundle-local.
func (c DeliveryCapability) MarshalJSON() ([]byte, error) {
	if c.Bundle != "" {
		return json.Marshal(struct {
			Bundle string `json:"bundle"`
		}{c.Bundle})
	}
	type plain DeliveryCapability
	return json.Marshal(plain(c))
}
func (a *App) resolveBundle(name, manifest string) (DeliveryCapability, error) {
	var result DeliveryCapability
	if err := a.ValidateDeliverySource(manifest, false); err != nil {
		return result, err
	}
	data, err := os.ReadFile(a.DeliveryPath(manifest))
	if err != nil {
		return result, err
	}
	var bundle DeliveryBundle
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&bundle); err != nil {
		return result, fmt.Errorf("%s: %w", manifest, err)
	}
	if bundle.Version != 1 || bundle.Name != name || bundle.Description == "" {
		return result, fmt.Errorf("некорректный bundle %s", manifest)
	}
	directory := filepath.ToSlash(filepath.Dir(manifest))
	source := func(path string) (string, error) {
		if !relativeDeliveryPath(path) {
			return "", fmt.Errorf("экспорт bundle %s выходит за его каталог: %s", name, path)
		}
		resolved := directory + "/" + path
		actual, err := filepath.EvalSymlinks(a.DeliveryPath(resolved))
		if err != nil {
			return "", err
		}
		root, err := filepath.EvalSymlinks(a.DeliveryPath(directory))
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, actual)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("экспорт bundle %s ссылается за его каталог", name)
		}
		return resolved, nil
	}
	if setup := bundle.Activation; setup != nil {
		if !Contains(bundle.Exports.MCP.Servers, setup.Server) || len(setup.Prompts) == 0 {
			return result, fmt.Errorf("bundle %s: activation требует экспортируемый MCP и вопросы", name)
		}
		seen := map[string]bool{}
		for _, prompt := range setup.Prompts {
			if prompt.Label == "" || seen[prompt.Key] || (prompt.Key != "url" && (!prompt.Secret || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(prompt.Key))) || (prompt.Key == "url" && prompt.Secret) || (prompt.Secret && prompt.Default != "") {
				return result, fmt.Errorf("bundle %s: некорректный вопрос activation", name)
			}
			seen[prompt.Key] = true
		}
	}
	result = DeliveryCapability{Bundle: manifest, Description: bundle.Description, Conflicts: bundle.Conflicts, Tools: bundle.Exports.Tools, Activation: bundle.Activation}
	for _, path := range bundle.Exports.Instructions {
		path, err = source(path)
		if err != nil {
			return result, err
		}
		result.Instructions = append(result.Instructions, path)
	}
	for _, path := range bundle.Exports.Skills {
		path, err = source(path)
		if err != nil {
			return result, err
		}
		result.Skills = append(result.Skills, path)
	}
	for _, doc := range bundle.Exports.Documents {
		doc.Source, err = source(doc.Source)
		if err != nil {
			return result, err
		}
		result.Documents = append(result.Documents, doc)
	}
	if bundle.Exports.MCP.Source != "" {
		result.MCPSource, err = source(bundle.Exports.MCP.Source)
		if err != nil {
			return result, err
		}
		result.MCP = bundle.Exports.MCP.Servers
		data, err := os.ReadFile(a.DeliveryPath(result.MCPSource))
		if err != nil {
			return result, err
		}
		var definitions struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err = json.Unmarshal(data, &definitions); err != nil {
			return result, err
		}
		for _, server := range result.MCP {
			if _, ok := definitions.Servers[server]; !ok {
				return result, fmt.Errorf("bundle %s экспортирует отсутствующий MCP %s", name, server)
			}
		}
	} else if len(bundle.Exports.MCP.Servers) > 0 {
		return result, fmt.Errorf("bundle %s: MCP source отсутствует", name)
	}
	return result, nil
}
