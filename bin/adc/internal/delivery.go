package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type DeliveryDocument struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
}

type DeliveryCapability struct {
	Activation   *CapabilityActivation `json:"activation,omitempty"`
	Bundle       string                `json:"bundle,omitempty"`
	MCPSource    string                `json:"mcpSource,omitempty"`
	Description  string                `json:"description"`
	Instructions []string              `json:"instructions"`
	Documents    []DeliveryDocument    `json:"documents"`
	MCP          []string              `json:"mcp"`
	Conflicts    []string              `json:"conflicts,omitempty"`
	Skills       []string              `json:"skills,omitempty"`
	Tools        []DeliveryTool        `json:"tools,omitempty"`
}

type DeliveryTool struct {
	Command   string `json:"command"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	Installer string `json:"installer"`
}

type DeliveryCatalog struct {
	Version int `json:"version"`
	Base    struct {
		Instructions []string `json:"instructions"`
		Skills       string   `json:"skills"`
		MCP          struct {
			Source  string   `json:"source"`
			Servers []string `json:"servers"`
		} `json:"mcp"`
		Documents []DeliveryDocument `json:"documents"`
	} `json:"base"`
	Capabilities   map[string]DeliveryCapability `json:"capabilities"`
	MCPDefinitions map[string]json.RawMessage    `json:"-"`
	MCPSources     map[string]string             `json:"-"`
}

func relativeDeliveryPath(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	return path != "" && !PortableAbs(path) && clean != ".." && !strings.HasPrefix(clean, ".."+string(os.PathSeparator)) && !strings.ContainsAny(path, ":\n\r\\")
}

// Every catalog source is relative to delivery/, regardless of its content type.
func (a *App) DeliveryPath(path string) string {
	return filepath.Join(a.Platform, "delivery", filepath.FromSlash(path))
}

func (a *App) ValidateDeliverySource(path string, directory bool) error {
	if !relativeDeliveryPath(path) {
		return fmt.Errorf("путь доставки выходит за delivery/: %s", path)
	}
	root, err := filepath.Abs(a.DeliveryPath(""))
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	source, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return fmt.Errorf("источник delivery/%s: %w", path, err)
	}
	relative, err := filepath.Rel(root, source)
	if err != nil || !relativeDeliveryPath(filepath.ToSlash(relative)) {
		return fmt.Errorf("источник delivery/%s ссылается за пределы delivery/", path)
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return fmt.Errorf("источник delivery/%s имеет неверный тип", path)
	}
	return nil
}

func (a *App) DeliveryCatalog() (DeliveryCatalog, error) {
	var catalog DeliveryCatalog
	data, err := os.ReadFile(filepath.Join(a.Platform, "delivery", "catalog.json"))
	if err != nil {
		return catalog, err
	}
	if err = json.Unmarshal(data, &catalog); err != nil {
		return catalog, fmt.Errorf("delivery/catalog.json: %w", err)
	}
	if catalog.Version != 1 || len(catalog.Base.Instructions) == 0 {
		return catalog, fmt.Errorf("delivery/catalog.json: неверная версия или пустой базовый контракт")
	}
	for name, capability := range catalog.Capabilities {
		if capability.Bundle != "" {
			resolved, err := a.resolveBundle(name, capability.Bundle)
			if err != nil {
				return catalog, err
			}
			catalog.Capabilities[name] = resolved
		}
	}
	files := append([]string{catalog.Base.MCP.Source}, catalog.Base.Instructions...)
	documents := append([]DeliveryDocument{}, catalog.Base.Documents...)
	for name, capability := range catalog.Capabilities {
		if !ValidID.MatchString(name) || len(capability.Instructions) == 0 {
			return catalog, fmt.Errorf("неверная capability %s", name)
		}
		for _, conflict := range capability.Conflicts {
			if _, ok := catalog.Capabilities[conflict]; !ok || conflict == name {
				return catalog, fmt.Errorf("неверный конфликт %s с %s", name, conflict)
			}
		}
		files = append(files, capability.Instructions...)
		if capability.MCPSource != "" {
			files = append(files, capability.MCPSource)
		}
		for _, skill := range capability.Skills {
			if err := a.ValidateDeliverySource(skill, true); err != nil {
				return catalog, err
			}
			files = append(files, skill+"/SKILL.md")
		}
		for _, tool := range capability.Tools {
			if !ValidID.MatchString(tool.Command) || !ValidID.MatchString(tool.Package) || tool.Installer != "uv" || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(tool.Version) {
				return catalog, fmt.Errorf("некорректная поставка инструмента %s", tool.Command)
			}
		}
		documents = append(documents, capability.Documents...)
	}
	for _, document := range documents {
		files = append(files, document.Source)
		if !relativeDeliveryPath(document.Target) {
			return catalog, fmt.Errorf("путь назначения выходит за проект: %s", document.Target)
		}
		if document.Mode != "link" && document.Mode != "copy" {
			return catalog, fmt.Errorf("неверный режим доставки %s", document.Mode)
		}
	}
	if err = a.ValidateDeliverySource(catalog.Base.Skills, true); err != nil {
		return catalog, err
	}
	for _, path := range files {
		if err = a.ValidateDeliverySource(path, false); err != nil {
			return catalog, err
		}
	}
	catalog.MCPDefinitions = map[string]json.RawMessage{}
	catalog.MCPSources = map[string]string{}
	sources := []string{catalog.Base.MCP.Source}
	names := make([]string, 0, len(catalog.Capabilities))
	for name := range catalog.Capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if source := catalog.Capabilities[name].MCPSource; source != "" && !Contains(sources, source) {
			sources = append(sources, source)
		}
	}
	for _, source := range sources {
		data, err = os.ReadFile(a.DeliveryPath(source))
		if err != nil {
			return catalog, err
		}
		var definitions struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err = json.Unmarshal(data, &definitions); err != nil || definitions.Servers == nil {
			return catalog, fmt.Errorf("delivery/%s: требуется объект mcpServers", source)
		}
		for name, definition := range definitions.Servers {
			if _, exists := catalog.MCPDefinitions[name]; exists {
				return catalog, fmt.Errorf("противоречивые определения MCP %s: %s и %s", name, catalog.MCPSources[name], source)
			}
			var object map[string]any
			if err := json.Unmarshal(definition, &object); err != nil || object == nil {
				return catalog, fmt.Errorf("MCP %s в %s должен быть объектом", name, source)
			}
			catalog.MCPDefinitions[name] = definition
			catalog.MCPSources[name] = source
		}
	}

	servers := append([]string{}, catalog.Base.MCP.Servers...)
	for _, capability := range catalog.Capabilities {
		servers = append(servers, capability.MCP...)
	}
	for _, server := range servers {
		definition, ok := catalog.MCPDefinitions[server]
		if !ok || string(definition) == "null" {
			return catalog, fmt.Errorf("MCP %s из каталога отсутствует в delivery/%s", server, catalog.Base.MCP.Source)
		}
	}
	return catalog, nil
}

func (a *App) InstructionText(capabilities []string) ([]byte, error) {
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		return nil, err
	}
	files := append([]string{}, catalog.Base.Instructions...)
	for _, name := range capabilities {
		capability, ok := catalog.Capabilities[name]
		if !ok {
			return nil, fmt.Errorf("неизвестная capability %s", name)
		}
		files = append(files, capability.Instructions...)
	}
	var result []byte
	seen := map[string]bool{}
	for _, name := range files {
		if seen[name] {
			continue
		}
		seen[name] = true
		data, err := os.ReadFile(a.DeliveryPath(name))
		if err != nil {
			return nil, err
		}
		result = append(result, '\n')
		result = append(result, data...)
	}
	return result, nil
}

func (a *App) DeliveryEnv(p Project) ([]string, error) {
	caps, err := a.ProjectCapabilities(p)
	if err != nil {
		return nil, err
	}
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		return nil, err
	}
	servers := append([]string{}, catalog.Base.MCP.Servers...)
	for _, cap := range caps {
		for _, server := range catalog.Capabilities[cap].MCP {
			if !Contains(servers, server) {
				servers = append(servers, server)
			}
		}
	}
	selection, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	return []string{
		"ADC_CAPABILITIES=" + strings.Join(caps, ","),
		"ADC_PLATFORM_MCP_SERVERS=" + string(selection),
		"ADC_COMPOSED_CONTEXT=1",
		"ADC_DELIVERY_ROOT=" + a.DeliveryPath(""),
		"AI_DEVCONTAINER_SKILLS=" + a.DeliveryPath(catalog.Base.Skills),
		"AI_DEVCONTAINER_MCP=" + a.DeliveryPath(catalog.Base.MCP.Source),
	}, nil
}
