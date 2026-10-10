package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"go.yaml.in/yaml/v3"
)

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func wireOrca(root string) error {
	if err := shared.MigrateProjectState(root); err != nil {
		return err
	}
	path := filepath.Join(root, "orca.yaml")
	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var config yaml.Node
	if len(original) > 0 {
		if err := yaml.Unmarshal(original, &config); err != nil {
			return err
		}
	}
	if len(config.Content) == 0 {
		config = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	mapping := config.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("orca.yaml должен содержать объект")
	}
	scripts := yamlField(mapping, "scripts")
	if scripts == nil || scripts.Tag == "!!null" {
		scripts = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSet(mapping, "scripts", scripts)
	}
	if scripts.Kind != yaml.MappingNode {
		return fmt.Errorf("orca.yaml scripts должен содержать объект")
	}
	setup := yamlField(scripts, "setup")
	if setup == nil {
		setup = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str"}
	}
	if setup.Kind != yaml.ScalarNode || setup.Tag != "!!str" {
		return fmt.Errorf("orca.yaml scripts.setup должен быть строкой")
	}
	start, end := "# adc:setup:start", "# adc:setup:end"
	body := start + "\nadc sync \"$PWD\" || exit $?\n" + end
	value := setup.Value
	if strings.Contains(value, start) || strings.Contains(value, end) {
		begin, finish := strings.Index(value, start), strings.Index(value, end)
		if begin < 0 || finish < begin {
			return fmt.Errorf("незавершённый ADC setup block в orca.yaml")
		}
		value = value[:begin] + body + value[finish+len(end):]
	} else {
		value = body + "\n" + value
	}
	policy := yamlField(mapping, "setupAgentStartupPolicy")
	if value == setup.Value && policy != nil && policy.Value == "wait-for-setup" {
		return nil
	}
	if len(original) > 0 {
		backup := filepath.Join(root, shared.ProjectStateDir, "backups", "orca.yaml")
		if _, err := os.Lstat(backup); os.IsNotExist(err) {
			if err := shared.AtomicWrite(backup, original, 0600); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	setup.Value, setup.Style = value, yaml.LiteralStyle
	yamlSet(scripts, "setup", setup)
	yamlSet(mapping, "setupAgentStartupPolicy", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "wait-for-setup"})
	data, err := yaml.Marshal(&config)
	if err != nil {
		return err
	}
	return shared.AtomicWrite(path, data, 0644)
}

func yamlField(mapping *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func yamlSet(mapping *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
