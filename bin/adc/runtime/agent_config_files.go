package runtime

import (
	"fmt"
	"go.yaml.in/yaml/v3"
	"os"
	"strings"
)

func yamlMapping(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var document yaml.Node
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if len(document.Content) == 0 || document.Content[0].Tag == "!!null" {
		document = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s должен содержать YAML объект", path)
	}
	return &document, nil
}
func yamlDelete(mapping *yaml.Node, key string) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}
func composeBlock(original, start, end, body string) (string, error) {
	begin, finish := strings.Index(original, start), strings.Index(original, end)
	if (begin >= 0) != (finish >= 0) || finish >= 0 && finish < begin {
		return "", fmt.Errorf("незавершённый управляемый блок %s", start)
	}
	block := ""
	if body != "" {
		block = start + "\n" + strings.TrimRight(body, "\n") + "\n" + end + "\n"
	}
	if begin >= 0 {
		return strings.TrimRight(original[:begin]+block+original[finish+len(end):], "\n") + "\n", nil
	}
	if block == "" {
		return original, nil
	}
	return strings.TrimRight(original, "\n") + "\n\n" + block, nil
}
