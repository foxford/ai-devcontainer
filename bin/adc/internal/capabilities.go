package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func (a *App) ProjectCapabilities(p Project) ([]string, error) {
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		return nil, err
	}
	var team struct {
		Capabilities []string `json:"capabilities"`
	}
	data, err := os.ReadFile(filepath.Join(p.RuntimePath, ".agents", "project.json"))
	if err == nil {
		if err = json.Unmarshal(data, &team); err != nil {
			return nil, fmt.Errorf(".agents/project.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var result []string
	for _, capability := range append(append([]string{}, p.Capabilities...), team.Capabilities...) {
		if _, ok := catalog.Capabilities[capability]; !ok {
			return nil, fmt.Errorf("неизвестная capability %s", capability)
		}
		if !Contains(result, capability) {
			result = append(result, capability)
		}
	}
	for _, name := range result {
		for _, conflict := range catalog.Capabilities[name].Conflicts {
			if Contains(result, conflict) {
				return nil, fmt.Errorf("несовместимые capabilities: %s и %s", name, conflict)
			}
		}
	}
	tools := map[string]DeliveryTool{}
	skills := map[string]string{}
	for _, name := range result {
		for _, tool := range catalog.Capabilities[name].Tools {
			if previous, ok := tools[tool.Command]; ok && previous != tool {
				return nil, fmt.Errorf("противоречивая поставка инструмента %s", tool.Command)
			}
			tools[tool.Command] = tool
		}
		for _, source := range catalog.Capabilities[name].Skills {
			skill := filepath.Base(source)
			if previous, ok := skills[skill]; ok && previous != source {
				return nil, fmt.Errorf("противоречивая поставка скилла %s", skill)
			}
			skills[skill] = source
		}
	}
	documents := append([]DeliveryDocument{}, catalog.Base.Documents...)
	for _, name := range result {
		documents = append(documents, catalog.Capabilities[name].Documents...)
	}
	byTarget := map[string]DeliveryDocument{}
	for _, document := range documents {
		if previous, exists := byTarget[document.Target]; exists && previous != document {
			return nil, fmt.Errorf("противоречивая доставка документа %s", document.Target)
		}
		byTarget[document.Target] = document
	}
	return result, nil
}
